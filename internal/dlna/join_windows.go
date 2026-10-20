package dlna

import (
	"net"
	"syscall"
)

// joinGroup une conn al grupo de SSDP también en la interfaz de ip.
func joinGroup(conn *net.UDPConn, ip net.IP) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		mreq := &syscall.IPMreq{}
		copy(mreq.Multiaddr[:], ssdpAddr.IP.To4())
		copy(mreq.Interface[:], ip.To4())
		serr = syscall.SetsockoptIPMreq(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq)
	})
	if err != nil {
		return err
	}
	return serr
}
