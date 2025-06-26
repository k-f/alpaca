package main

import (
	"testing"
)

func TestRuleEngine_CheckURL(t *testing.T) {
	tests := []struct {
		name          string
		allowPatterns []string
		denyPatterns  []string
		url           string
		wantDecision  DecisionType
	}{
		// Deny Rules
		{"Deny exact domain", []string{}, []string{"blocked.com"}, "http://blocked.com/path", Denied},
		{"Deny wildcard domain", []string{}, []string{"*.blocked.com"}, "http://sub.blocked.com", Denied},
		{"Deny path", []string{}, []string{"example.com/private/*"}, "http://example.com/private/secret", Denied},
		{"Deny host with port", []string{}, []string{"blocked.com:8080"}, "http://blocked.com:8080/path", Denied},
		{"Deny takes precedence", []string{"allowed.com"}, []string{"allowed.com"}, "http://allowed.com", Denied},
		{"Deny specific subpath, allow parent", []string{"example.com/*"}, []string{"example.com/deny/*"}, "http://example.com/deny/this", Denied},
		{"Deny specific subpath, allow parent (no match)", []string{"example.com/*"}, []string{"example.com/deny/*"}, "http://example.com/allow/this", Allowed},


		// Allow Rules
		{"Allow exact domain", []string{"allowed.com"}, []string{}, "http://allowed.com/path", Allowed},
		{"Allow wildcard domain", []string{"*.allowed.com"}, []string{}, "http://sub.allowed.com", Allowed},
		{"Allow path", []string{"example.com/public/*"}, []string{}, "http://example.com/public/resource", Allowed},
		{"Allow host with port", []string{"allowed.com:8080"}, []string{}, "http://allowed.com:8080/path", Allowed},
		{"Allow domain, deny different domain", []string{"good.com"}, []string{"bad.com"}, "http://good.com/index", Allowed},

		// Prompt User
		{"No match, prompt", []string{"allowed.com"}, []string{"denied.com"}, "http://other.com", PromptUser},
		{"Allow pattern no match path", []string{"example.com/specific"}, []string{}, "http://example.com/other", PromptUser},
		{"Deny pattern no match path", []string{}, []string{"example.com/specific"}, "http://example.com/other", PromptUser},
		{"Empty rules, prompt", []string{}, []string{}, "http://anything.com", PromptUser},

		// Specificity and path matching
		{"Allow *.domain.com, check sub.sub.domain.com", []string{"*.domain.com"}, []string{}, "http://sub.sub.domain.com", Allowed}, // path.Match * matches one segment
		{"Allow *.domain.com, check domain.com (no match)", []string{"*.domain.com"}, []string{}, "http://domain.com", PromptUser}, // * requires at least one char for segment
		{"Allow * (host only), check domain.com", []string{"*"}, []string{}, "http://domain.com", Allowed},
		{"Allow * (host only), check domain.com/path", []string{"*"}, []string{}, "http://domain.com/path", Allowed}, // host matches, path is irrelevant for this pattern type
		{"Allow */path, check domain.com/path", []string{"*/path"}, []string{}, "http://domain.com/path", Allowed},
		{"Allow example.com/*, check example.com (no match)", []string{"example.com/*"}, []string{}, "http://example.com", PromptUser}, // requires /
		{"Allow example.com/*, check example.com/", []string{"example.com/*"}, []string{}, "http://example.com/", Allowed}, // path.Match with * can match empty
		{"Allow example.com, check example.com/ (host match)", []string{"example.com"}, []string{}, "http://example.com/", Allowed},


		// URLs with queries and fragments
		{"Allow with query", []string{"example.com/query"}, []string{}, "http://example.com/query?param=val", Allowed},
		{"Deny with fragment", []string{}, []string{"example.com/frag"}, "http://example.com/frag#section", Denied},

		// HTTPS scheme
		{"Allow https", []string{"secure.com/*"}, []string{}, "https://secure.com/page", Allowed},
		{"Deny https", []string{}, []string{"block.secure.com"}, "https://block.secure.com", Denied},

		// Malformed URL (RuleEngine relies on url.Parse, if it fails, CheckURL returns Denied)
		{"Malformed URL input", []string{}, []string{}, "http://%", Denied}, // url.Parse will error
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := NewRuleEngine(tt.allowPatterns, tt.denyPatterns)
			if decision := re.CheckURL(tt.url); decision != tt.wantDecision {
				t.Errorf("RuleEngine.CheckURL() for url '%s' with allow %v deny %v = %v, want %v", tt.url, tt.allowPatterns, tt.denyPatterns, decision, tt.wantDecision)
			}
		})
	}
}

func TestRuleEngine_UpdateRules(t *testing.T) {
	re := NewRuleEngine([]string{"initial.allow.com"}, []string{"initial.deny.com"})

	// Check initial state
	if decision := re.CheckURL("http://initial.allow.com"); decision != Allowed {
		t.Errorf("Initial allow rule failed. Got %v, want Allowed", decision)
	}
	if decision := re.CheckURL("http://initial.deny.com"); decision != Denied {
		t.Errorf("Initial deny rule failed. Got %v, want Denied", decision)
	}

	// Update rules
	newAllow := []string{"new.allow.com"}
	newDeny := []string{"new.deny.com"}
	re.UpdateRules(newAllow, newDeny)

	// Check new state
	if decision := re.CheckURL("http://new.allow.com"); decision != Allowed {
		t.Errorf("New allow rule failed after update. Got %v, want Allowed", decision)
	}
	if decision := re.CheckURL("http://new.deny.com"); decision != Denied {
		t.Errorf("New deny rule failed after update. Got %v, want Denied", decision)
	}

	// Check that old rules are gone
	if decision := re.CheckURL("http://initial.allow.com"); decision != PromptUser {
		t.Errorf("Old allow rule still active after update. Got %v, want PromptUser", decision)
	}
	if decision := re.CheckURL("http://initial.deny.com"); decision != PromptUser {
		t.Errorf("Old deny rule still active after update. Got %v, want PromptUser", decision)
	}
}
