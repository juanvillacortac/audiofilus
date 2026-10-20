package dlna

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Las acciones UPnP llegan por SOAP: un POST con un sobre XML cuyo cuerpo es
// un elemento con el nombre de la acción y un hijo por argumento. La
// respuesta es otro sobre con <AcciónResponse> y los argumentos de salida, o
// un fallo SOAP con el código de error UPnP.

// upnpError es un error de una acción, con su código UPnP (401 acción no
// válida, 402 argumentos, 701 transición no válida...).
type upnpError struct {
	Code int
	Desc string
}

func (e *upnpError) Error() string { return fmt.Sprintf("upnp error %d: %s", e.Code, e.Desc) }

var (
	errInvalidAction = &upnpError{401, "Invalid Action"}
	errInvalidArgs   = &upnpError{402, "Invalid Args"}
	errNoSuchObject  = &upnpError{701, "No such object"}
	errTransition    = &upnpError{701, "Transition not available"}
	errSeekMode      = &upnpError{710, "Seek mode not supported"}
	errSeekTarget    = &upnpError{711, "Illegal seek target"}
	errBadInstance   = &upnpError{718, "Invalid InstanceID"}
	errResource      = &upnpError{716, "Resource not found"}
)

// arg es un argumento de salida (en orden: algunos clientes lo esperan).
type arg struct{ name, value string }

// action es una acción recibida: su nombre y sus argumentos.
type action struct {
	Name string
	Args map[string]string
}

// parseAction lee el sobre SOAP de r (sin depender de los prefijos de
// espacio de nombres, que cada cliente pone a su manera).
func parseAction(r io.Reader) (*action, error) {
	dec := xml.NewDecoder(io.LimitReader(r, 1<<20))
	depth, inBody := 0, false
	var act *action
	var cur string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case t.Name.Local == "Body":
				inBody = true
			case inBody && act == nil:
				act = &action{Name: t.Name.Local, Args: map[string]string{}}
			case act != nil && cur == "":
				cur = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if cur != "" {
				text.Write(t)
			}
		case xml.EndElement:
			depth--
			if cur != "" && t.Name.Local == cur {
				act.Args[cur] = text.String()
				cur = ""
			}
		}
	}
	if act == nil {
		return nil, fmt.Errorf("no action in SOAP body")
	}
	return act, nil
}

// soapResponse escribe la respuesta de una acción del servicio serviceType.
func soapResponse(w http.ResponseWriter, serviceType, name string, out []arg) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%sResponse xmlns:u="%s">`, name, serviceType)
	for _, a := range out {
		fmt.Fprintf(&b, "<%s>%s</%s>", a.name, xmlEscape(a.value), a.name)
	}
	fmt.Fprintf(&b, `</u:%sResponse>`, name)
	b.WriteString(`</s:Body></s:Envelope>`)
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("EXT", "")
	_, _ = w.Write(b.Bytes())
}

// soapFault escribe el fallo de una acción.
func soapFault(w http.ResponseWriter, e *upnpError) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, e.Code, xmlEscape(e.Desc))
}

// handler es lo que atiende las acciones de un servicio.
type handler func(a *action) ([]arg, error)

// serveControl atiende el POST de control de un servicio.
func serveControl(serviceType string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a, err := parseAction(r.Body)
		if err != nil {
			soapFault(w, errInvalidAction)
			return
		}
		out, err := h(a)
		if err != nil {
			ue, ok := err.(*upnpError)
			if !ok {
				ue = &upnpError{501, err.Error()}
			}
			soapFault(w, ue)
			return
		}
		soapResponse(w, serviceType, a.Name, out)
	}
}

// xmlEscape escapa texto para meterlo en XML.
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
