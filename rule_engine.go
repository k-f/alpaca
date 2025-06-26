package main

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// DecisionType represents the outcome of a rule check.
type DecisionType int

const (
	// Allowed means the URL matches an allow rule or no rules were matched and default is allow.
	Allowed DecisionType = iota
	// Denied means the URL matches a deny rule.
	Denied
	// PromptUser means the URL does not match any allow or deny rules, requiring user interaction.
	PromptUser
)

// RuleEngine checks URLs against allow and deny lists.
type RuleEngine struct {
	allowAlwaysPatterns []string
	denyAlwaysPatterns  []string
}

// NewRuleEngine creates a new RuleEngine with the given rule patterns.
// The patterns are expected to be in a format compatible with path.Match.
func NewRuleEngine(allowPatterns, denyPatterns []string) *RuleEngine {
	return &RuleEngine{
		allowAlwaysPatterns: allowPatterns,
		denyAlwaysPatterns:  denyPatterns,
	}
}

// UpdateRules allows dynamic updating of the rules in the engine.
// This would be called after the ConfigManager reloads the config.
func (re *RuleEngine) UpdateRules(allowPatterns, denyPatterns []string) {
	re.allowAlwaysPatterns = allowPatterns
	re.denyAlwaysPatterns = denyPatterns
}

// CheckURL determines if a given URL string should be allowed, denied, or prompt the user.
// It checks deny rules first, then allow rules.
// URL matching supports:
// 1. Exact domain match: "example.com"
// 2. Wildcard domain match: "*.example.com"
// 3. Path-based match: "example.com/some/path/*" or "*.example.com/some/path"
func (re *RuleEngine) CheckURL(rawURL string) DecisionType {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		fmt.Printf("Error parsing URL '%s': %v. Defaulting to Deny.\n", rawURL, err)
		return Denied // Or handle error more gracefully
	}

	// Normalize: Use hostname (which includes port if specified) or host for matching.
	// For simplicity, we'll primarily use parsedURL.Host which includes port.
	// And parsedURL.Path for path matching.
	// Example: for "https://www.example.com:8080/path/to/resource?query=val"
	// hostWithPath will be "www.example.com:8080/path/to/resource"
	hostWithPath := parsedURL.Host + parsedURL.Path

	// Check Deny Rules first
	for _, pattern := range re.denyAlwaysPatterns {
		// Try matching full host + path
		matched, _ := path.Match(pattern, hostWithPath)
		if matched {
			return Denied
		}
		// If pattern doesn't contain '/', try matching only against the host
		if !strings.Contains(pattern, "/") {
			matchedHost, _ := path.Match(pattern, parsedURL.Host)
			if matchedHost {
				return Denied
			}
		}
	}

	// Check Allow Rules
	for _, pattern := range re.allowAlwaysPatterns {
		matched, _ := path.Match(pattern, hostWithPath)
		if matched {
			return Allowed
		}
		if !strings.Contains(pattern, "/") {
			matchedHost, _ := path.Match(pattern, parsedURL.Host)
			if matchedHost {
				return Allowed
			}
		}
	}

	// If no rules matched, prompt the user
	return PromptUser
}
