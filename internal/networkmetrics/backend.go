package networkmetrics

import (
	"context"
	"fmt"
	"net"

	gnet "github.com/shirou/gopsutil/v4/net"
)

type Counter struct {
	Interface string
	BytesRecv uint64
	BytesSent uint64
}

type Backend interface {
	InterfaceForIP(net.IP) (string, error)
	Counter(context.Context, string) (Counter, error)
}

type GopsutilBackend struct{}

func NewGopsutilBackend() GopsutilBackend { return GopsutilBackend{} }

func (GopsutilBackend) InterfaceForIP(ip net.IP) (string, error) {
	if ip == nil {
		return "", fmt.Errorf("route local IP unavailable")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	matched := ""
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var candidate net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				candidate = v.IP
			case *net.IPAddr:
				candidate = v.IP
			}
			if candidate == nil || !candidate.Equal(ip) {
				continue
			}
			if matched != "" && matched != iface.Name {
				return "", fmt.Errorf("route interface ambiguous")
			}
			matched = iface.Name
		}
	}
	if matched == "" {
		return "", fmt.Errorf("route interface unavailable")
	}
	return matched, nil
}

func (GopsutilBackend) Counter(ctx context.Context, interfaceName string) (Counter, error) {
	stats, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return Counter{}, err
	}
	for _, stat := range stats {
		if stat.Name == interfaceName {
			return Counter{Interface: stat.Name, BytesRecv: stat.BytesRecv, BytesSent: stat.BytesSent}, nil
		}
	}
	return Counter{}, fmt.Errorf("route interface counter unavailable")
}
