package upstream

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"time"
)

// TLSInfo captures live diagnostic details from the most recent upstream handshake or HTTP connection.
type TLSInfo struct {
	Transport     string `json:"transport"`
	Protocol      string `json:"protocol"`
	CipherSuite   string `json:"cipherSuite"`
	PeerSubject   string `json:"peerSubject"`
	PeerKeyBits   string `json:"peerKeyBits"`
	CertNotBefore string `json:"certNotBefore"`
	CertNotAfter  string `json:"certNotAfter"`
	CertExpired   bool   `json:"certExpired"`
	Target        string `json:"target"`
	UpdatedAt     string `json:"updatedAt"`
}

func buildLegacyTLSConfig(preferLegacy bool) *tls.Config {
	stdSuites := tls.CipherSuites()
	insecureSuites := tls.InsecureCipherSuites()

	allSuites := make([]uint16, 0, len(stdSuites)+len(insecureSuites))
	if preferLegacy {
		for _, s := range insecureSuites {
			allSuites = append(allSuites, s.ID)
		}
		for _, s := range stdSuites {
			allSuites = append(allSuites, s.ID)
		}
	} else {
		for _, s := range stdSuites {
			allSuites = append(allSuites, s.ID)
		}
		for _, s := range insecureSuites {
			allSuites = append(allSuites, s.ID)
		}
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS10,
		MaxVersion:         tls.VersionTLS13,
		CipherSuites:       allSuites,
		InsecureSkipVerify: true,
		Renegotiation:      tls.RenegotiateFreelyAsClient,
	}

	if preferLegacy {
		// Strip TLS 1.3 and post-quantum curves so fragile embedded servers receive a compact ClientHello.
		cfg.MaxVersion = tls.VersionTLS12
		cfg.SessionTicketsDisabled = true
		cfg.CurvePreferences = []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		}
	}

	return cfg
}

func extractTLSInfo(cs tls.ConnectionState, endpoint Endpoint) TLSInfo {
	info := TLSInfo{
		Transport:   "HTTPS",
		Protocol:    tlsVersionName(cs.Version),
		CipherSuite: tls.CipherSuiteName(cs.CipherSuite),
		Target:      endpoint.URL(),
		UpdatedAt:   time.Now().Format("15:04:05"),
	}

	if len(cs.PeerCertificates) > 0 {
		cert := cs.PeerCertificates[0]
		subject := cert.Subject.CommonName
		if subject == "" {
			subject = cert.Subject.String()
		}
		if subject == "" {
			subject = "(self-signed / blank subject)"
		}
		info.PeerSubject = subject
		info.CertNotBefore = cert.NotBefore.UTC().Format("2006-01-02")
		info.CertNotAfter = cert.NotAfter.UTC().Format("2006-01-02")
		now := time.Now()
		info.CertExpired = now.After(cert.NotAfter) || now.Before(cert.NotBefore)

		switch pub := cert.PublicKey.(type) {
		case *rsa.PublicKey:
			info.PeerKeyBits = fmt.Sprintf("%d-bit RSA", pub.N.BitLen())
		case *ecdsa.PublicKey:
			info.PeerKeyBits = fmt.Sprintf("%d-bit ECDSA", pub.Curve.Params().BitSize)
		default:
			info.PeerKeyBits = string(cert.PublicKeyAlgorithm.String())
		}
	}

	return info
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionSSL30:
		return "SSLv3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}
