package main

import (
	"log"
	"net" // For net.Dialer
	"net/http"
	"net/url" // Required for proxyFunc type from Alpaca
	"strings" // For checking upstream proxy scheme
	"time"    // For upstream proxy timeout
)

// SecureProxyHandler wraps the original Alpaca ProxyHandler and adds rule-based access control.
type SecureProxyHandler struct {
	// originalAlpacaHandler ProxyHandler // Instance of the original Alpaca handler
	// Instead of embedding the struct, we'll take its components or replicate functionality.
	// This is because ProxyHandler is not exported if it's in the main package of another module.
	// However, since we are in the *same* package 'main', we can directly use and create ProxyHandler.

	originalHandler ProxyHandler // The original Alpaca ProxyHandler instance

	ruleEngine    *RuleEngine
	uiController  *UIController
	configManager *ConfigManager
}

// NewSecureProxyHandler creates a new SecureProxyHandler.
// It takes the necessary components for Alpaca's original ProxyHandler,
// plus our custom components.
func NewSecureProxyHandler(
	auth *authenticator,
	proxyFn func(*http.Request) (*url.URL, error), // alpaca's getProxyFromContext
	blockFn func(string), // alpaca's proxyFinder.blockProxy
	re *RuleEngine,
	uic *UIController,
	cfgm *ConfigManager,
) *SecureProxyHandler {
	// Create an instance of Alpaca's original ProxyHandler
	// This instance will be used to handle requests once our rules allow them.
	originalAlpacaProxyHandler := NewProxyHandler(auth, proxyFn, blockFn)

	return &SecureProxyHandler{
		originalHandler: originalAlpacaProxyHandler,
		ruleEngine:      re,
		uiController:    uic,
		configManager:   cfgm,
	}
}

// WrapHandler is a middleware that decides whether a request should be handled
// by our SecureProxyHandler's ServeHTTP (for proxy requests) or passed to the next handler (for PAC files, etc.).
// This structure mirrors Alpaca's original ProxyHandler.WrapHandler.
func (sph *SecureProxyHandler) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pass CONNECT requests and absolute-form URIs to our SecureProxyHandler.ServeHTTP.
		// If the request URL has a scheme, it is an absolute-form URI (RFC 7230 Section 5.3.2).
		if r.Method == http.MethodConnect || r.URL.Scheme != "" {
			sph.ServeHTTP(w, r) // This is OUR ServeHTTP
			return
		}
		// The request URI is an origin-form or asterisk-form target which we
		// handle as an origin server (RFC 7230 5.3). authority-form URIs
		// are only for CONNECT, which has already been dispatched to the ProxyHandler.
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP is the core logic for handling proxied requests.
// It checks rules, prompts the user if necessary, and then either blocks
// or delegates to the original Alpaca ProxyHandler.
func (sph *SecureProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestURLString := r.URL.String()
	if r.Method == http.MethodConnect {
		// For CONNECT requests, the URL is in the Host field.
		requestURLString = "https://" + r.Host // Assume HTTPS for CONNECT target
		// Note: Alpaca's original handler also uses r.Host for CONNECT.
		// We need to ensure rule matching works correctly for this.
		// Our rule engine expects full URLs like "domain.com/path" or "domain.com".
		// For CONNECT to "example.com:443", requestURLString becomes "https://example.com:443"
	}


	log.Printf("SecureProxyHandler: Processing request for URL: %s (Method: %s)", requestURLString, r.Method)

	// 1. Check rules
	decision := sph.ruleEngine.CheckURL(requestURLString)

	if decision == Denied {
		log.Printf("SecureProxyHandler: Denied URL %s by rule.", requestURLString)
		http.Error(w, "Blocked by corporate policy (matched deny_always rule)", http.StatusForbidden)
		return
	}

	if decision == PromptUser {
		log.Printf("SecureProxyHandler: Prompting user for URL %s.", requestURLString)
		userDecision, ruleToAdd := sph.uiController.RequestUserDecision(requestURLString)

		switch userDecision {
		case UserDecisionDenyOnce:
			log.Printf("SecureProxyHandler: Denied URL %s by user (Deny Once).", requestURLString)
			http.Error(w, "Blocked by user (Deny Once)", http.StatusForbidden)
			return
		case UserDecisionDenyAlways:
			log.Printf("SecureProxyHandler: Denied URL %s by user (Deny Always). Adding rule: %s", requestURLString, ruleToAdd)
			if ruleToAdd == "" { // Should be pre-filled by uiController or logic here
				parsedURL, err := url.Parse(requestURLString)
				if err == nil {
					ruleToAdd = parsedURL.Hostname()
				} else {
					ruleToAdd = requestURLString // Fallback, though less ideal
				}
			}
			err := sph.configManager.AddDenyRule(ruleToAdd)
			if err != nil {
				log.Printf("SecureProxyHandler: Error adding deny rule '%s': %v", ruleToAdd, err)
				// Continue to block the request even if saving the rule fails
			}
			sph.ruleEngine.UpdateRules(sph.configManager.GetConfig().AllowAlways, sph.configManager.GetConfig().DenyAlways)
			http.Error(w, "Blocked by user (Deny Always)", http.StatusForbidden)
			return
		case UserDecisionAllowAlways:
			log.Printf("SecureProxyHandler: Allowed URL %s by user (Allow Always). Adding rule: %s", requestURLString, ruleToAdd)
			// ruleToAdd now comes validated from uiController, should not be empty.
			err := sph.configManager.AddAllowRule(ruleToAdd)
			if err != nil {
				log.Printf("SecureProxyHandler: Error adding allow rule '%s': %v", ruleToAdd, err)
				// Continue to allow the request for this instance
			}
			sph.ruleEngine.UpdateRules(sph.configManager.GetConfig().AllowAlways, sph.configManager.GetConfig().DenyAlways)
			// Fall through to allow the request
		case UserDecisionAllowOnce:
			log.Printf("SecureProxyHandler: Allowed URL %s by user (Allow Once).", requestURLString)
			// Fall through to allow the request
		case UserDecisionError:
			log.Printf("SecureProxyHandler: Error obtaining user decision for %s. Blocking request.", requestURLString)
			http.Error(w, "Failed to get user decision", http.StatusInternalServerError)
			return
		default:
			log.Printf("SecureProxyHandler: Unknown user decision. Blocking request %s.", requestURLString)
			http.Error(w, "Unknown decision outcome", http.StatusInternalServerError)
			return
		}
	}

	// If execution reaches here, the request is allowed (either by rule_engine directly or by user prompt).
	log.Printf("SecureProxyHandler: Allowed URL %s. Forwarding to original Alpaca handler.", requestURLString)

	// Before calling original handler, check if we have a custom upstream proxy.
	upstreamProxyURL := sph.configManager.GetUpstreamProxy()
	if upstreamProxyURL != "" {
		// Modify the request to use the upstream proxy.
		// The originalAlpacaHandler's transport uses `getProxyFromContext` which might use PAC files.
		// If our upstreamProxyURL is set, we should override that.
		// This requires modifying the transport used by originalHandler.
		// Alpaca's NewProxyHandler sets up a transport:
		// tr := &http.Transport{Proxy: proxy, TLSClientConfig: tlsClientConfig}
		// We need to potentially replace this `proxy` function or the transport itself.

		// For simplicity in this step, we'll assume the originalAlpacaHandler's
		// proxyFunc (getProxyFromContext) will be eventually configured or we modify it.
		// The PRD says: "It must be able to forward all allowed traffic to an upstream proxy".
		// Alpaca itself is designed to find and use system proxies or PAC configured proxies.
		// If our config.yaml's upstream_proxy is set, it should take precedence.

		// Let's check if sph.originalHandler.transport.Proxy can be set.
		// sph.originalHandler.transport is *http.Transport. Its Proxy field is a func(*Request) (*url.URL, error).

		parsedUpstream, err := url.Parse(upstreamProxyURL)
		if err != nil {
			log.Printf("SecureProxyHandler: Invalid upstream_proxy URL '%s': %v. Using Alpaca's default proxy logic.", upstreamProxyURL, err)
		} else {
			// Ensure scheme is http or https for typical proxies. Socks might need different transport.
			if strings.ToLower(parsedUpstream.Scheme) != "http" && strings.ToLower(parsedUpstream.Scheme) != "https" {
                 log.Printf("SecureProxyHandler: Upstream proxy '%s' has unsupported scheme '%s'. Using Alpaca's default proxy logic.", upstreamProxyURL, parsedUpstream.Scheme)
			} else {
				log.Printf("SecureProxyHandler: Using configured upstream proxy: %s", upstreamProxyURL)
				// Create a new transport for this request if upstream proxy is specified
                // This ensures other parts of alpaca aren't permanently changed if not desired.
                // However, originalHandler.transport is shared. Modifying it here affects all subsequent uses by originalHandler.
                // This is likely the desired behavior: if upstream is set, all allowed traffic goes through it.
				sph.originalHandler.transport.Proxy = http.ProxyURL(parsedUpstream)
                // Set a timeout for the upstream proxy connection.
                // Default transport has no timeout, which can cause hangs.
                if sph.originalHandler.transport.DialContext == nil {
                     // Setting a default dialer if none exists.
                     // This is a basic dialer; more sophisticated (e.g., from net/http.DefaultTransport) might be better.
                    dialer := &net.Dialer{
                        Timeout:   30 * time.Second, // Connection timeout
                        KeepAlive: 30 * time.Second,
                    }
                    sph.originalHandler.transport.DialContext = dialer.DialContext
                }
                sph.originalHandler.transport.ResponseHeaderTimeout = 60 * time.Second // Timeout for receiving headers
                 sph.originalHandler.transport.TLSHandshakeTimeout = 10 * time.Second // TLS handshake timeout
			}
		}
	}


	// Delegate to the original Alpaca ProxyHandler's ServeHTTP method.
	sph.originalHandler.ServeHTTP(w, r)
}
