package redact

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// builtinRules returns the ordered default rule set. Order matters: broad
// credential rules run before identifiers, and PEM/JWT run before generic
// token patterns.
func builtinRules(cfg Config) []Rule {
	hexMin := cfg.HexMinLength
	if hexMin <= 0 {
		hexMin = 64
	}

	rules := []Rule{
		{
			Name:    "pem-private-key",
			Pattern: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
			Repl:    "<REDACTED:pem-private-key>",
		},
		{
			Name:    "jwt",
			Pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
			Repl:    "<REDACTED:jwt>",
		},
		{
			Name:    "bearer-token",
			Pattern: regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{10,}`),
			Repl:    "<REDACTED:bearer-token>",
		},
		{
			Name: "brand-token",
			Pattern: regexp.MustCompile(`\b(?:sk-ant-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9_-]{16,}|` +
				`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|` +
				`glpat-[A-Za-z0-9_-]{16,}|xox[baprs]-[A-Za-z0-9-]{10,}|` +
				`AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,}|` +
				`ya29\.[0-9A-Za-z_-]{20,}|npm_[A-Za-z0-9]{20,}|pypi-[A-Za-z0-9_-]{20,}|` +
				`hf_[A-Za-z0-9]{20,}|dckr_pat_[A-Za-z0-9_-]{20,})\b`),
			Repl: "<REDACTED:brand-token>",
		},
		{
			Name:    "authorization-header",
			Pattern: regexp.MustCompile(`(?im)\b(Authorization)\s*:\s*[^\r\n]+`),
			Repl:    "$1: <REDACTED:authorization>",
		},
		{
			Name:    "set-cookie-header",
			Pattern: regexp.MustCompile(`(?im)^[ \t]*(Set-Cookie)\s*:\s*[^\r\n]+`),
			Repl:    "$1: <REDACTED:set-cookie>",
		},
		{
			Name:    "cookie-header",
			Pattern: regexp.MustCompile(`(?im)^[ \t]*(Cookie)\s*:\s*[^\r\n]+`),
			Repl:    "$1: <REDACTED:cookie>",
		},
		{
			Name:    "x-api-key-header",
			Pattern: regexp.MustCompile(`(?im)\b(X-Api-Key)\s*:\s*[^\r\n]+`),
			Repl:    "$1: <REDACTED:x-api-key>",
		},
		{
			Name:    "credential-assignment",
			Pattern: regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)\s*[:=]\s*["']?[^\s"',;}]{8,}`),
			Repl:    "$1=<REDACTED:credential>",
		},
		{
			Name:    "url-credentials",
			Pattern: regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`),
			Repl:    "$1<REDACTED:url-credentials>@",
		},
		{
			Name:    "windows-user-path",
			Pattern: regexp.MustCompile(`(?i)\b[A-Z]:\\Users\\[^\\\s"']+`),
			Repl:    "~",
		},
		{
			Name:    "unix-user-path",
			Pattern: regexp.MustCompile(`(?i)(?:/home/|/Users/)[^/\s"']+`),
			Repl:    "~",
		},
	}

	if !cfg.DisableEmails {
		rules = append(rules, Rule{
			Name:    "email",
			Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
			Repl:    "<REDACTED:email>",
		})
	}
	if !cfg.DisablePhones {
		rules = append(rules, Rule{
			Name:    "phone-cn",
			Pattern: regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`),
			Repl:    "<REDACTED:phone>",
			Guard:   phoneGuard,
		})
	}
	if !cfg.DisableIP {
		rules = append(rules, Rule{
			Name:    "ipv4",
			Pattern: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
			Repl:    "<REDACTED:ip>",
			Guard:   ipv4Guard(cfg.RedactPrivateIP),
		})
	}

	// Long hex is intentionally last and conservative: by default the
	// threshold leaves git SHAs (40) and UUIDs (36) intact.
	longHex := regexp.MustCompile(fmt.Sprintf(`\b[0-9a-fA-F]{%d,}\b`, hexMin))
	rules = append(rules, Rule{
		Name:    "long-hex",
		Pattern: longHex,
		Repl:    "<REDACTED:long-hex>",
		Guard:   longHexGuard(hexMin),
	})

	return rules
}

func phoneGuard(_ string, before, after string) bool {
	if before != "" && isASCIIDigit(before[len(before)-1]) {
		return false
	}
	if after != "" && isASCIIDigit(after[0]) {
		return false
	}
	return true
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func ipv4Guard(redactPrivate bool) Guard {
	return func(match, before, _ string) bool {
		// Avoid version strings such as "v1.2.3.4" and "version 1.2.3.4".
		ctx := strings.ToLower(strings.TrimSpace(before))
		if strings.HasSuffix(ctx, "version") || strings.HasSuffix(ctx, "v") {
			return false
		}

		parts := strings.Split(match, ".")
		if len(parts) != 4 {
			return false
		}
		var octets [4]int
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > 255 {
				return false
			}
			octets[i] = n
		}
		if octets[0] == 127 || octets[0] == 0 {
			return false
		}
		if octets[0] == 255 && octets[1] == 255 && octets[2] == 255 && octets[3] == 255 {
			return false
		}

		private := octets[0] == 10 ||
			(octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31) ||
			(octets[0] == 192 && octets[1] == 168) ||
			(octets[0] == 169 && octets[1] == 254) ||
			(octets[0] == 100 && octets[1] >= 64 && octets[1] <= 127)
		if private {
			return redactPrivate
		}
		return true
	}
}

func longHexGuard(min int) Guard {
	return func(match, before, after string) bool {
		if len(match) < min {
			return false
		}
		hasDigit, hasHexLetter := false, false
		for i := 0; i < len(match); i++ {
			c := match[i]
			switch {
			case c >= '0' && c <= '9':
				hasDigit = true
			case (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
				hasHexLetter = true
			default:
				return false
			}
		}
		if !hasDigit || !hasHexLetter {
			return false
		}

		context := strings.ToLower(before + " " + after)
		for _, safe := range []string{
			"checksum", "sha256", "sha-256", "sha1", "sha-1", "md5",
			"digest", "integrity", "etag", "blob hash", "commit", "revision",
			"uuid", "guid",
		} {
			if strings.Contains(context, safe) {
				return false
			}
		}
		return true
	}
}
