package cast

import (
	"context"
	"sync"
	"time"
)

// Session es la música sonando en un aparato: carga la canción (servida por
// el Server), la controla y mira su estado cada segundo. Cuando la canción
// acaba pone la siguiente que se le dio con Queue (en UPnP, si el aparato
// sabe, se la deja preparada antes para que no haya corte) y avisa por
// Events. Close para el aparato y suelta la conexión: la música vuelve a
// sonar en este equipo (eso lo decide quien la usa).

// EventKind es qué pasó.
type EventKind int

const (
	StateChanged EventKind = iota // el aparato cambió de estado (Status)
	Advanced                      // empezó la siguiente (Item)
	Finished                      // la canción acabó y no había siguiente
	Failed                        // el aparato dejó de contestar (Err)
)

// Event es un aviso de la sesión.
type Event struct {
	Kind   EventKind
	Status Status
	Item   Item
	Err    error
}

// Session es una conexión con un aparato.
type Session struct {
	srv *Server
	dev Device
	p   player

	// ForceTranscode manda todo como WAV (para aparatos quisquillosos).
	ForceTranscode bool

	mu       sync.Mutex
	cur      Item
	next     *Item
	nextMed  *media // la siguiente ya preparada en el aparato (SetNext)
	last     Status
	loadedAt time.Time
	fails    int

	events chan Event
	quit   chan struct{}
	wg     sync.WaitGroup
}

// NewSession prepara una sesión con d (aún no manda nada).
func NewSession(srv *Server, d Device) (*Session, error) {
	s := &Session{srv: srv, dev: d, events: make(chan Event, 16), quit: make(chan struct{})}
	switch d.Kind {
	case Chromecast:
		s.p = newCastPlayer(d)
	case MPD:
		s.p = newMPDPlayer(d)
	default:
		s.p = newUPnPPlayer(d)
	}
	s.wg.Add(1)
	go s.watch()
	return s, nil
}

// Device es el aparato de la sesión.
func (s *Session) Device() Device { return s.dev }

// Events son los avisos (hay que leerlos: si se llenan, se pierden).
func (s *Session) Events() <-chan Event { return s.events }

func (s *Session) emit(e Event) {
	select {
	case s.events <- e:
	default:
	}
}

// Cast manda it al aparato y empieza a sonar desde start segundos.
func (s *Session) Cast(ctx context.Context, it Item, start float64) error {
	m, err := s.srv.URLFor(s.dev, it, s.ForceTranscode)
	if err != nil {
		return err
	}
	if err := s.p.Load(ctx, m, start); err != nil {
		return err
	}
	s.mu.Lock()
	s.cur, s.next, s.nextMed, s.loadedAt, s.fails = it, nil, nil, time.Now(), 0
	s.last = Status{State: Loading, Position: start, Duration: it.Duration, Volume: s.last.Volume}
	s.mu.Unlock()
	return nil
}

// Queue deja it como la siguiente: suena al acabar la actual.
func (s *Session) Queue(ctx context.Context, it Item) error {
	s.mu.Lock()
	s.next, s.nextMed = &it, nil
	s.mu.Unlock()
	m, err := s.srv.URLFor(s.dev, it, s.ForceTranscode)
	if err != nil {
		return err
	}
	if err := s.p.SetNext(ctx, m); err == nil {
		s.mu.Lock()
		s.nextMed = &m
		s.mu.Unlock()
	}
	return nil
}

// Current es lo que suena.
func (s *Session) Current() Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *Session) Pause(ctx context.Context) error  { return s.p.Pause(ctx) }
func (s *Session) Resume(ctx context.Context) error { return s.p.Play(ctx) }
func (s *Session) Seek(ctx context.Context, sec float64) error {
	err := s.p.Seek(ctx, sec)
	if err == nil {
		s.mu.Lock()
		s.last.Position = sec
		s.mu.Unlock()
	}
	return err
}
func (s *Session) SetVolume(ctx context.Context, v float64) error { return s.p.SetVolume(ctx, v) }

// Status es lo último que dijo el aparato (se pregunta cada segundo).
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Close para el aparato y suelta la conexión.
func (s *Session) Close(ctx context.Context) error {
	select {
	case <-s.quit:
		return nil
	default:
	}
	close(s.quit)
	s.wg.Wait()
	_ = s.p.Stop(ctx)
	return s.p.Close()
}

// watch pregunta el estado cada segundo y avanza a la siguiente al acabar.
func (s *Session) watch() {
	defer s.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		st, err := s.p.Status(ctx)
		cancel()
		if err != nil {
			s.mu.Lock()
			s.fails++
			fails := s.fails
			s.mu.Unlock()
			if fails == 3 {
				s.emit(Event{Kind: Failed, Err: err})
			}
			continue
		}
		s.step(st)
	}
}

// step mira un estado nuevo: avisa si cambió y decide si la canción acabó.
func (s *Session) step(st Status) {
	s.mu.Lock()
	prev := s.last
	if st.Duration == 0 {
		st.Duration = s.cur.Duration
	}
	s.last, s.fails = st, 0
	young := time.Since(s.loadedAt) < 4*time.Second // recién cargada: aún "parada"
	nearEnd := prev.Duration > 0 && prev.Position >= prev.Duration-3
	next, nextMed := s.next, s.nextMed
	s.mu.Unlock()
	if st.State != prev.State {
		s.emit(Event{Kind: StateChanged, Status: st})
	}
	switch {
	case nextMed != nil && prev.State == Playing && nearEnd && st.Position < prev.Position-5:
		// El aparato pasó solo a la que se le dejó preparada.
		s.mu.Lock()
		s.cur, s.next, s.nextMed, s.loadedAt = *next, nil, nil, time.Now()
		s.mu.Unlock()
		s.emit(Event{Kind: Advanced, Item: *next, Status: st})
	case st.State == Stopped && prev.State == Playing && !young && (nearEnd || prev.Duration == 0):
		if next != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.Cast(ctx, *next, 0)
			cancel()
			if err != nil {
				s.emit(Event{Kind: Failed, Err: err})
				return
			}
			s.emit(Event{Kind: Advanced, Item: *next})
			return
		}
		s.emit(Event{Kind: Finished, Status: st})
	}
}
