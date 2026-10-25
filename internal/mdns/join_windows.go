package mdns

import (
	"net"
	"syscall"
)

// joinGroup une conn al grupo de mDNS también en la interfaz de ip.
func joinGroup(conn *net.UDPConn, ip net.IP) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		mreq := &syscall.IPMreq{}
		copy(mreq.Multiaddr[:], groupAddr.IP.To4())
		copy(mreq.Interface[:], ip.To4())
		serr = syscall.SetsockoptIPMreq(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq)
	})
	if err != nil {
		return err
	}
	return serr
}

// setTTL pone el TTL de IP a 255, como pide mDNS (RFC 6762, 11): Avahi y
// otros descartan lo que llega con otro. Y vuelve a encender el bucle de
// multicast, que ListenMulticastUDP apaga: sin él, lo que se manda al grupo
// no lo ven los otros programas del mismo equipo (Avahi, otro audiofilus).
func setTTL(conn *net.UDPConn) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
		if serr == nil {
			serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_TTL, 255)
		}
		if serr == nil {
			serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)
		}
	})
	if err != nil {
		return err
	}
	return serr
}
