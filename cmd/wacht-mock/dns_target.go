package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const (
	dnsFixtureHost = "smoke-dns.wacht.test"
	dnsFixtureIP   = "172.29.0.53"

	dnsTypeA     = 1
	dnsClassINET = 1

	dnsResponseBit   = 1 << 15
	dnsRecursionWant = 1 << 8
	dnsRecursionOK   = 1 << 7
	dnsRCodeNoError  = 0
	dnsRCodeNXDomain = 3
)

// dnsTarget is a tiny smoke-only DNS server. It intercepts exactly one fixture
// hostname and proxies every other query to Docker's embedded DNS so normal
// service discovery keeps working inside the smoke stack.
type dnsTarget struct {
	hostNormalized string
	upstreamAddr   string
	answer         [4]byte
	// state is just an up/down toggle; using atomic keeps the HTTP control path
	// and the DNS serving goroutines synchronized without a mutex.
	state       atomic.Int32
	udpConn     net.PacketConn
	tcpListener net.Listener
}

type dnsQuestion struct {
	id       uint16
	flags    uint16
	name     string
	qType    uint16
	qClass   uint16
	question []byte
}

// newDNSTarget binds both UDP and TCP on port 53 because resolvers may fall
// back to TCP even for simple lookups. The smoke check path should tolerate
// either transport.
func newDNSTarget(listenAddr, upstreamAddr, host, answerIP string) (*dnsTarget, error) {
	ip := net.ParseIP(answerIP).To4()
	if ip == nil {
		return nil, fmt.Errorf("dns target requires IPv4 answer, got %q", answerIP)
	}

	udpConn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen udp on %s: %w", listenAddr, err)
	}

	tcpListener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("listen tcp on %s: %w", listenAddr, err)
	}

	target := &dnsTarget{
		hostNormalized: normalizeDNSName(host),
		upstreamAddr:   upstreamAddr,
		udpConn:        udpConn,
		tcpListener:    tcpListener,
	}
	copy(target.answer[:], ip)
	target.state.Store(stateUp)

	go target.serveUDP()
	go target.serveTCP()

	return target, nil
}

func (t *dnsTarget) close() error {
	var errs []string
	if err := t.udpConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		errs = append(errs, err.Error())
	}
	if err := t.tcpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

func (t *dnsTarget) status() string {
	if t.state.Load() == stateDown {
		return "down"
	}
	return "up"
}

func (t *dnsTarget) setStatus(status string) error {
	switch status {
	case "up":
		t.state.Store(stateUp)
		return nil
	case "down":
		t.state.Store(stateDown)
		return nil
	default:
		return fmt.Errorf("%w %q", errUnsupportedStatus, status)
	}
}

func (t *dnsTarget) serveUDP() {
	buf := make([]byte, 1500)
	for {
		n, addr, err := t.udpConn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("dns udp read error: %s", err)
			return
		}

		response, err := t.handleMessage(buf[:n], false)
		if err != nil {
			log.Printf("dns udp query error: %s", err)
			continue
		}
		if _, err := t.udpConn.WriteTo(response, addr); err != nil {
			log.Printf("dns udp write error: %s", err)
		}
	}
}

func (t *dnsTarget) serveTCP() {
	for {
		conn, err := t.tcpListener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("dns tcp accept error: %s", err)
			return
		}
		go t.handleTCPConn(conn)
	}
}

// TCP DNS frames each message with a 2-byte length prefix. One connection may
// carry multiple queries, so we stay in the loop until the client closes it.
func (t *dnsTarget) handleTCPConn(conn net.Conn) {
	defer conn.Close()

	var sizeBuf [2]byte
	for {
		if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("dns tcp read size error: %s", err)
			}
			return
		}

		size := int(binary.BigEndian.Uint16(sizeBuf[:]))
		if size == 0 {
			return
		}

		query := make([]byte, size)
		if _, err := io.ReadFull(conn, query); err != nil {
			log.Printf("dns tcp read body error: %s", err)
			return
		}

		response, err := t.handleMessage(query, true)
		if err != nil {
			log.Printf("dns tcp query error: %s", err)
			return
		}

		binary.BigEndian.PutUint16(sizeBuf[:], uint16(len(response)))
		if _, err := conn.Write(sizeBuf[:]); err != nil {
			log.Printf("dns tcp write size error: %s", err)
			return
		}
		if _, err := conn.Write(response); err != nil {
			log.Printf("dns tcp write body error: %s", err)
			return
		}
	}
}

// handleMessage either answers the one smoke fixture hostname directly or
// forwards the query upstream unchanged. That keeps this server narrowly scoped
// instead of reimplementing a general resolver.
func (t *dnsTarget) handleMessage(query []byte, overTCP bool) ([]byte, error) {
	question, err := parseDNSQuestion(query)
	if err != nil {
		return nil, err
	}
	if normalizeDNSName(question.name) != t.hostNormalized {
		return t.proxy(query, overTCP)
	}

	if t.status() == "down" {
		return buildDNSResponse(question, dnsRCodeNXDomain, nil), nil
	}

	if question.qType == dnsTypeA && question.qClass == dnsClassINET {
		return buildDNSResponse(question, dnsRCodeNoError, buildARecord(t.answer)), nil
	}

	return buildDNSResponse(question, dnsRCodeNoError, nil), nil
}

func (t *dnsTarget) proxy(query []byte, overTCP bool) ([]byte, error) {
	if overTCP {
		return proxyDNSTCP(t.upstreamAddr, query)
	}
	return proxyDNSUDP(t.upstreamAddr, query)
}

// parseDNSQuestion pulls out the first and only supported question from a raw
// DNS packet. Smoke traffic is simple, so rejecting multi-question packets is
// fine and keeps the response builder straightforward.
func parseDNSQuestion(query []byte) (dnsQuestion, error) {
	if len(query) < 12 {
		return dnsQuestion{}, fmt.Errorf("dns query too short: %d bytes", len(query))
	}
	if binary.BigEndian.Uint16(query[4:6]) != 1 {
		return dnsQuestion{}, fmt.Errorf("dns query must contain exactly one question")
	}

	name, next, err := parseDNSName(query, 12)
	if err != nil {
		return dnsQuestion{}, err
	}
	if len(query) < next+4 {
		return dnsQuestion{}, io.ErrUnexpectedEOF
	}

	questionEnd := next + 4
	question := make([]byte, questionEnd-12)
	copy(question, query[12:questionEnd])

	return dnsQuestion{
		id:       binary.BigEndian.Uint16(query[0:2]),
		flags:    binary.BigEndian.Uint16(query[2:4]),
		name:     name,
		qType:    binary.BigEndian.Uint16(query[next : next+2]),
		qClass:   binary.BigEndian.Uint16(query[next+2 : next+4]),
		question: question,
	}, nil
}

// parseDNSName decodes the label sequence in the question section. We reject
// compression pointers here because normal client queries do not need them and
// keeping the parser linear makes it easier to audit.
func parseDNSName(query []byte, offset int) (string, int, error) {
	labels := make([]string, 0, 4)
	i := offset

	for {
		if i >= len(query) {
			return "", 0, io.ErrUnexpectedEOF
		}
		n := int(query[i])
		i++
		if n == 0 {
			return strings.Join(labels, "."), i, nil
		}
		if n&0xc0 != 0 {
			return "", 0, fmt.Errorf("compressed dns query names are not supported")
		}
		if i+n > len(query) {
			return "", 0, io.ErrUnexpectedEOF
		}
		labels = append(labels, strings.ToLower(string(query[i:i+n])))
		i += n
	}
}

// buildDNSResponse echoes the original question back and optionally appends one
// answer record. That is enough for the smoke check, which only cares whether
// a hostname resolves at all.
func buildDNSResponse(question dnsQuestion, rCode uint16, answer []byte) []byte {
	flags := dnsResponseBit | dnsRecursionOK | (question.flags & dnsRecursionWant) | (question.flags & 0x7800) | rCode
	answerCount := uint16(0)
	if len(answer) > 0 {
		answerCount = 1
	}

	response := make([]byte, 12, 12+len(question.question)+len(answer))
	binary.BigEndian.PutUint16(response[0:2], question.id)
	binary.BigEndian.PutUint16(response[2:4], flags)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], answerCount)
	binary.BigEndian.PutUint16(response[8:10], 0)
	binary.BigEndian.PutUint16(response[10:12], 0)
	response = append(response, question.question...)
	response = append(response, answer...)
	return response
}

// buildARecord emits a single IPv4 answer. The leading 0xc00c is a standard
// compression pointer back to the query name at byte offset 12.
func buildARecord(ip [4]byte) []byte {
	record := make([]byte, 16)
	record[0] = 0xc0
	record[1] = 0x0c
	binary.BigEndian.PutUint16(record[2:4], dnsTypeA)
	binary.BigEndian.PutUint16(record[4:6], dnsClassINET)
	binary.BigEndian.PutUint32(record[6:10], 1)
	binary.BigEndian.PutUint16(record[10:12], 4)
	copy(record[12:16], ip[:])
	return record
}

// Unknown hostnames still need to resolve through Docker service discovery, so
// UDP queries are forwarded to Docker's embedded resolver instead of failing.
func proxyDNSUDP(upstreamAddr string, query []byte) ([]byte, error) {
	conn, err := net.DialTimeout("udp", upstreamAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// TCP proxying mirrors the DNS-over-TCP framing rules with the same length
// prefix used by handleTCPConn above.
func proxyDNSTCP(upstreamAddr string, query []byte) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", upstreamAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}

	var sizeBuf [2]byte
	binary.BigEndian.PutUint16(sizeBuf[:], uint16(len(query)))
	if _, err := conn.Write(sizeBuf[:]); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(sizeBuf[:]))
	response := make([]byte, size)
	if _, err := io.ReadFull(conn, response); err != nil {
		return nil, err
	}
	return response, nil
}

func normalizeDNSName(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}
