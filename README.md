# Secure LLM Agent Proxy

## Overview

The Secure LLM Agent Proxy is a desktop application designed to mitigate the security risks associated with using Large Language Model (LLM) agents on corporate devices. It gives users granular, real-time control over the LLM agent's network access by intercepting HTTP/HTTPS requests, checking them against a configurable set of rules, and prompting the user for a decision on any untrusted destination.

This project is built by extending the [SAMUONG/alpaca](https://github.com/SAMUONG/alpaca) Go proxy and uses the [Fyne](https://fyne.io/) framework for its cross-platform user interface.

## Features

*   **HTTP/HTTPS Proxy**: Acts as a local proxy server that other applications can be configured to use.
*   **Rule-Based Filtering**: Utilizes `allow_always` and `deny_always` lists in a configuration file (`config.yaml`) to automatically permit or block requests. Supports wildcard (`*`) and path-based matching (e.g., `*.example.com`, `example.com/path/*`).
*   **Interactive User Prompts**: For requests not matching any predefined rule, a clear UI prompt allows the user to:
    *   **Allow Once**: Permit the specific request.
    *   **Deny Once**: Block the specific request.
    *   **Allow Always**: Permit the request and add a user-specified rule (e.g., domain or path) to the `allow_always` list.
    *   **Deny Always**: Block the request and add its domain to the `deny_always` list.
*   **User Configuration**: Rules and settings are managed via a `config.yaml` file stored in the standard user application configuration directory.
*   **Upstream Proxy Support**: Can forward all allowed traffic to a designated upstream corporate proxy (e.g., Zscaler).
*   **System Tray Management**: Provides a system tray/menu bar icon for quick access to:
    *   Open Config File: Opens `config.yaml` in the default text editor.
    *   Quit: Shuts down the proxy application.
*   **Cross-Platform**: Designed for macOS and Windows. Linux users may also find it functional, though system tray behavior can vary by desktop environment.

## How It Works

1.  Applications (e.g., LLM agents) are configured to send their network traffic through this proxy.
2.  The proxy intercepts each outgoing HTTP/HTTPS request.
3.  The request URL is checked against the `deny_always` list first. If a match is found, the request is blocked.
4.  If not denied, the URL is checked against the `allow_always` list. If a match is found, the request is permitted.
5.  If no rule matches, a UI prompt is displayed to the user.
6.  Based on the user's decision, the request is allowed or blocked. If "Allow Always" or "Deny Always" is chosen, the `config.yaml` file and in-memory rules are updated.
7.  Allowed requests are then forwarded to their destination, potentially via a configured upstream proxy.

## Installation & Build

### Prerequisites

*   Go (version 1.18 or newer recommended)
*   Standard C compiler (required by Fyne for some platforms/features, usually pre-installed on macOS/Linux, may need setup on Windows e.g., via MinGW)

### Build Command

1.  Clone the repository (or ensure you have the source code).
2.  Navigate to the project directory.
3.  Run the build command:
    ```bash
    go build .
    ```
    This will produce an executable file (e.g., `secure-llm-agent-proxy` or `secure-llm-agent-proxy.exe`).

### Packaging (Optional)

For a more native app experience (e.g., `.app` bundle on macOS):

1.  Install the Fyne command-line tool:
    ```bash
    go install fyne.io/fyne/v2/cmd/fyne@latest
    ```
2.  Run the packaging command (example for macOS):
    ```bash
    fyne package -os darwin -icon YourIcon.png
    ```
    Replace `YourIcon.png` with the path to an icon file.

#### macOS Specifics for Backgrounding:

To make the macOS `.app` bundle run as a background agent (no Dock icon):
1.  After running `fyne package`, locate the generated `.app` bundle (e.g., `SecureLLMAgentProxy.app`).
2.  Open/Edit the `Contents/Info.plist` file within the bundle.
3.  Add or ensure the following key-value pair exists:
    ```xml
    <key>LSUIElement</key>
    <string>1</string>
    ```

## Configuration

The proxy is configured using a `config.yaml` file located in your user's application configuration directory. The application will create a default one on first run if it doesn't exist.

*   **macOS**: `~/Library/Application Support/SecureLLMAgentProxy/config.yaml`
*   **Linux**: `~/.config/SecureLLMAgentProxy/config.yaml` (or follows XDG Base Directory Specification)
*   **Windows**: `%APPDATA%\SecureLLMAgentProxy\config.yaml` (e.g., `C:\Users\YourUser\AppData\Roaming\SecureLLMAgentProxy\config.yaml`)

You can also use the "Open Config File" option from the system tray menu to locate and open it.

### `config.yaml` Structure:

```yaml
allow_always:
  - "*.trusted.com"        # Allows subdomains of trusted.com
  - "internaltool.net/api/*" # Allows specific paths on internaltool.net
  - "exactdomain.com"      # Allows only exactdomain.com (and its paths if not further restricted by pattern)
deny_always:
  - "ads.doubleclick.net"
  - "*.annoyingtracker.com"
upstream_proxy: "http://your-corporate-proxy.example.com:8080" # Optional: URL of your corporate proxy
```

### Rule Syntax:

Rules use `path.Match` style globbing:
*   `*`: Matches any sequence of characters except `/`. For example, `*.example.com` matches `api.example.com` but not `example.com`. `example.com/*` matches `example.com/foo`.
*   `?`: Matches any single character except `/`.
*   To match a domain and all its subdomains and paths, you might use rules like `example.com` (for the domain itself) and `*.example.com` (for subdomains). If you want to allow all paths under `example.com`, use `example.com/*`.
*   The matching logic first attempts to match the pattern against the full `host:port/path`. If the pattern does not contain a `/`, it also attempts to match against just the `host:port`.

## Usage

1.  Run the compiled executable (e.g., `./secure-llm-agent-proxy` or double-click the `.exe` / `.app`).
2.  Configure your LLM agent or other target application to use this proxy for HTTP and HTTPS traffic. The proxy listens by default on:
    *   **Address**: `localhost`
    *   **Port**: `3128`
    (These can be changed using the `-l <address>` and `-p <port>` command-line flags inherited from Alpaca.)
3.  When the configured application makes a network request to an untrusted URL, a dialog prompt will appear.
4.  Choose one of the four options: "Allow Once", "Deny Once", "Allow Always...", or "Deny Always".
5.  Use the system tray icon to open the configuration file for manual rule editing or to quit the application.

## License

This project is licensed under the Apache License 2.0. See the `LICENSE` file for details.
(Assumes a LICENSE file from Alpaca or a new one is present)
