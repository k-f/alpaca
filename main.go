// Copyright 2019, 2021, 2022 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
)

var BuildVersion string

func whoAmI() string {
	me, err := user.Current()
	if err != nil {
		return ""
	}
	return me.Username
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
	host := flag.String("l", "localhost", "address to listen on")
	port := flag.Int("p", 3128, "port number to listen on")
	pacurl := flag.String("C", "", "url of proxy auto-config (pac) file")
	domain := flag.String("d", "", "domain of the proxy account (for NTLM auth)")
	username := flag.String("u", whoAmI(), "username of the proxy account (for NTLM auth)")
	printHash := flag.Bool("H", false, "print hashed NTLM credentials for non-interactive use")
	version := flag.Bool("version", false, "print version number")
	flag.Parse()

	if *version {
		fmt.Println("Alpaca", BuildVersion)
		os.Exit(0)
	}

	var src credentialSource
	if *domain != "" {
		src = fromTerminal().forUser(*domain, *username)
	} else if value := os.Getenv("NTLM_CREDENTIALS"); value != "" {
		src = fromEnvVar(value)
	} else {
		src = fromKeyring()
	}

	var a *authenticator
	if src != nil {
		var err error
		a, err = src.getCredentials()
		if err != nil {
			log.Printf("Credentials not found, disabling proxy auth: %v", err)
		}
	}

	if *printHash {
		if a == nil {
			fmt.Println("Please specify a domain (using -d) and username (using -u)")
			os.Exit(1)
		}
		fmt.Printf("# Add this to your ~/.profile (or equivalent) and restart your shell\n")
		fmt.Printf("NTLM_CREDENTIALS=%q; export NTLM_CREDENTIALS\n", a)
		os.Exit(0)
	}

	// Initialize our custom components
	cfgManager, err := NewConfigManager()
	if err != nil {
		log.Fatalf("Failed to initialize ConfigManager: %v", err)
	}
	log.Println("ConfigManager initialized.")

	currentConfig := cfgManager.GetConfig()
	ruleEngine := NewRuleEngine(currentConfig.AllowAlways, currentConfig.DenyAlways)
	log.Println("RuleEngine initialized.")

	uiController := NewUIController(cfgManager) // Pass ConfigManager
	log.Println("UIController initialized.")

	// Start the Fyne app in a goroutine. uiController.Run() will block.
	go func() {
		uiController.Run()
		log.Println("Fyne application stopped.")
	}()

	errch := make(chan error)

	// Pass our components to createServer
	s := createServer(*host, *port, *pacurl, a, cfgManager, ruleEngine, uiController)

	for _, network := range networks(*host) {
		go func(network string) {
			l, err := net.Listen(network, s.Addr)
			if err != nil {
				errch <- err
			} else {
				log.Printf("Listening on %s %s", network, s.Addr)
				errch <- s.Serve(l)
			}
		}(network)
	}

	// Wait for server error or Fyne app to close (though Fyne is usually blocking main)
	// For robust shutdown, might need signal handling to call uiController.Quit()
	log.Fatal(<-errch)
}

func createServer(host string, port int, pacurl string, alpacaAuth *authenticator,
	cfgManager *ConfigManager, ruleEngine *RuleEngine, uiCtrl *UIController) *http.Server {
	pacWrapper := NewPACWrapper(PACData{Port: port})
	proxyFinder := NewProxyFinder(pacurl, pacWrapper)

	// Original Alpaca proxy handler
	originalAlpacaHandler := NewProxyHandler(alpacaAuth, getProxyFromContext, proxyFinder.blockProxy)

	// Our custom handler that wraps the original Alpaca handler
	customHandler := NewSecureProxyHandler(originalAlpacaHandler, ruleEngine, uiCtrl, cfgManager)

	mux := http.NewServeMux()
	pacWrapper.SetupHandlers(mux)

	// build the handler by wrapping middleware upon middleware
	var handler http.Handler = mux
	handler = RequestLogger(handler)
	// Replace originalAlpacaHandler.WrapHandler with our customHandler.WrapHandler
	// Our customHandler.WrapHandler will internally decide when to call originalAlpacaHandler.ServeHTTP
	// For now, let's assume SecureProxyHandler itself is the primary handler for proxy requests
	// and it will call the original alpaca logic for allowed requests.
	// The structure of WrapHandler in Alpaca's proxy.go suggests we need a similar WrapHandler
	// in our secure_proxy_handler.go that will then call our main ServeHTTP logic.
	// Or, our SecureProxyHandler's ServeHTTP becomes the main logic.
	// Let's make SecureProxyHandler implement http.Handler directly for the core logic,
	// and it will be wrapped by proxyFinder and others.

	// The key is to insert our handler before the original proxy logic.
	// Alpaca's NewProxyHandler returns a ProxyHandler struct which has WrapHandler and ServeHTTP.
	// We want our logic to execute before its ServeHTTP.

	// Option 1: Our handler wraps theirs.
	// handler = customHandler.WrapAroundOriginal(originalAlpacaHandler, handler)
	// This implies customHandler needs a specific method.

	// Option 2: Our handler is part of the chain, replacing the original one.
	// The original WrapHandler in Alpaca's proxy.go is:
	// handler = proxyHandler.WrapHandler(handler)
	// This means proxyHandler.ServeHTTP is called for specific requests.
	// We need our SecureProxyHandler to be called instead of alpaca's proxyHandler.ServeHTTP for those.

	// Let's define SecureProxyHandler to have a ServeHTTP method.
	// And we'll insert it into the chain.
	// The original proxyHandler.WrapHandler is a filter for when to call its own ServeHTTP.
	// We need to replicate that filtering or make our handler smart enough.

	// The `proxyHandler.WrapHandler(handler)` is a bit tricky.
	// `handler` at that point is `mux`. `proxyHandler.WrapHandler` returns a new handler
	// that calls `proxyHandler.ServeHTTP` for proxy requests or `mux.ServeHTTP` for others.
	// We need to replace `proxyHandler.ServeHTTP` with `customHandler.ServeHTTP`.
	// So, `customHandler` will be the one whose `ServeHTTP` is called by `proxyHandler.WrapHandler`.
	// This means `customHandler` should be the `http.Handler` that `proxyHandler.WrapHandler` operates on
	// for the requests it decides are proxy requests.

	// Let's look at proxy.go's WrapHandler:
	// func (ph ProxyHandler) WrapHandler(next http.Handler) http.Handler {
	//  return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	//    if req.Method == http.MethodConnect || req.URL.Scheme != "" {
	//      ph.ServeHTTP(w, req) // <--- We want our SecureProxyHandler.ServeHTTP here
	//      return
	//    }
	//    next.ServeHTTP(w, req) // This 'next' is 'mux' in createServer
	//  })
	// }
	// This means we should pass our SecureProxyHandler's ServeHTTP to Alpaca's WrapHandler.
	// Or, more cleanly, our SecureProxyHandler takes the *original* Alpaca ProxyHandler
	// and calls its methods when a request is allowed.

	// Let's make SecureProxyHandler the primary decision maker.
	// It will be an http.Handler.
	var mainLogicHandler http.Handler = customHandler // customHandler will implement ServeHTTP

	// The original alpaca `proxyHandler.WrapHandler(mux)` is what decides if a request is for proxying or for PAC files.
	// We need to maintain that. The `proxyHandler` (originalAlpacaHandler here) has this `WrapHandler`.
	// We want our `customHandler.ServeHTTP` to be called INSTEAD of `originalAlpacaHandler.ServeHTTP`.
	// This implies `customHandler` should replace `originalAlpacaHandler` in the call to `WrapHandler`.
	// BUT `customHandler` needs the `originalAlpacaHandler` to delegate to.

	// This is the new structure:
	// 1. NewSecureProxyHandler takes originalAlpacaHandler, ruleEngine, etc.
	// 2. NewSecureProxyHandler has a ServeHTTP method.
	// 3. This ServeHTTP method implements our logic (check rules, prompt user).
	// 4. If allowed, it calls originalAlpacaHandler.ServeHTTP (or a more specific method like .proxyRequest).

	// So, the `customHandler` *is* the handler that `WrapHandler` should invoke for proxy requests.
	// The `WrapHandler` from `originalAlpacaHandler` can be used if we pass `customHandler` as `next`
	// to a modified version of it, but that's messy.

	// Simpler: `customHandler` itself will be wrapped.
	// `proxyFinder.WrapHandler` should wrap our `customHandler`.
	// `RequestLogger` should wrap that.
	// `AddContextID` should wrap that.
	// The `mux` for PAC files should be separate or `customHandler` needs to delegate to it.

	// Let's re-evaluate the chain:
	// original: AddContextID(proxyFinder.WrapHandler(proxyHandler.WrapHandler(RequestLogger(mux))))
	// (Order might be slightly different, bottom-up application)
	// mux -> RequestLogger -> proxyHandler.WrapHandler -> proxyFinder.WrapHandler -> AddContextID

	// proxyHandler.WrapHandler(next) calls ph.ServeHTTP or next.ServeHTTP.
	// Here, 'next' is RequestLogger(mux).
	// We want our logic inside ph.ServeHTTP. So, our `SecureProxyHandler` will have its own `ServeHTTP`.
	// This `SecureProxyHandler` will internally call the *original* Alpaca's `ServeHTTP` (or parts of it)
	// if the request is allowed by our rules.

	// So, `proxyHandler` in `createServer` should become our `customHandler`.
	// `customHandler` will be of a type that embeds or uses the original alpaca handler's logic.

	// Replacing:
	// proxyHandler := NewProxyHandler(a, getProxyFromContext, proxyFinder.blockProxy)
	// with:
	// secureHandler := NewSecureProxyHandler(NewProxyHandler(a, getProxyFromContext, proxyFinder.blockProxy), ruleEngine, uiCtrl, cfgManager)
	// And then use secureHandler in the chain:
	// handler = secureHandler.WrapHandler(handler) // If SecureProxyHandler adopts WrapHandler structure
	// OR if SecureProxyHandler directly implements ServeHTTP for all its logic:

	// Let SecureProxyHandler implement http.Handler.
	// The `proxyHandler.WrapHandler(mux)` is the part that decides if it's a proxy request or a PAC request.
	// This means `proxyHandler` (the instance of Alpaca's ProxyHandler) is what we need to replace with our own.
	// Our `SecureProxyHandler` will have the same signature for `WrapHandler` and `ServeHTTP`.
	// Its `ServeHTTP` will contain our logic and then call the *original* Alpaca `ServeHTTP` logic.

	// So, `proxy_handler.go` will define `SecureProxyHandler` with `ServeHTTP` and `WrapHandler`.
	// `NewSecureProxyHandler` will create it.
	// In `createServer`, we instantiate `SecureProxyHandler` instead of Alpaca's `ProxyHandler`.

	secureHandler := NewSecureProxyHandler(
		alpacaAuth,
		getProxyFromContext, // This is Alpaca's way of finding upstream proxy, might need to adapt for our config
		proxyFinder.blockProxy, // Alpaca's block function
		ruleEngine,
		uiCtrl,
		cfgManager,
	)

	var handler http.Handler = mux
	handler = RequestLogger(handler)
	handler = secureHandler.WrapHandler(handler) // Our SecureProxyHandler now needs a WrapHandler
	handler = proxyFinder.WrapHandler(handler)
	handler = AddContextID(handler)


	return &http.Server{
		// Set the addr to host(defaults to localhost) : port(defaults to 3128)
		Addr:    net.JoinHostPort(host, strconv.Itoa(port)),
		Handler: handler,
		// TODO: Implement HTTP/2 support. In the meantime, set TLSNextProto to a non-nil
		// value to disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
}

func networks(hostname string) []string {
	if hostname == "" {
		return []string{"tcp"}
	}
	addrs, err := net.LookupIP(hostname)
	if err != nil {
		log.Fatal(err)
	}
	nets := make([]string, 0, 2)
	ipv4 := false
	ipv6 := false
	for _, addr := range addrs {
		// addr == net.IPv4len doesn't work because all addrs use IPv6 format.
		if addr.To4() != nil {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	if ipv4 {
		nets = append(nets, "tcp4")
	}
	if ipv6 {
		nets = append(nets, "tcp6")
	}
	return nets
}
