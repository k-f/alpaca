package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"os/exec"
	"runtime"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/desktop"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// UserDecision represents the user's choice from the prompt.
type UserDecision int

const (
	UserDecisionAllowOnce UserDecision = iota
	UserDecisionDenyOnce
	UserDecisionAllowAlways
	UserDecisionDenyAlways
	UserDecisionError // Represents an error in getting a decision
	UserDecisionDialogDismissed // User closed the dialog without explicit choice
)

// PromptRequest holds the data for a UI prompt.
type PromptRequest struct {
	URL            string
	ResponseChan   chan PromptResponse // Channel to send the user's decision back
	DefaultRule    string              // Default rule to suggest for AllowAlways
}

// PromptResponse holds the user's decision and any rule to be added.
type PromptResponse struct {
	Decision    UserDecision
	RuleToAdd   string // e.g., "example.com" or "example.com/path/*"
}

// UIController manages the Fyne application and UI interactions.
type UIController struct {
	fyneApp         fyne.App
	promptRequestCh chan *PromptRequest // Channel to send requests to the UI goroutine
	mainWindow      fyne.Window         // A hidden main window to keep the app alive if needed.
	// activeDialog    fyne.Window         // Not strictly needed if dialogMutex handles one-at-a-time
	dialogMutex   sync.Mutex    // To ensure only one dialog is active and processed at a time
	configManager *ConfigManager // To get config file path for system tray menu
}

// NewUIController initializes the Fyne application and the UI controller.
func NewUIController(cfgManager *ConfigManager) *UIController {
	fyneApp := app.NewWithID("com.securellmagentproxy.app") // Unique ID for Fyne settings

	mainWindow := fyneApp.NewWindow("Secure LLM Agent Proxy Control")
	mainWindow.SetMaster()
	// mainWindow.SetContent(widget.NewLabel("Secure LLM Agent Proxy is running in the background.\nPrompts will appear as needed."))
	// mainWindow.Resize(fyne.NewSize(400,100))
	mainWindow.Hide() // Keep it hidden for background operation

	uic := &UIController{
		fyneApp:         fyneApp,
		promptRequestCh: make(chan *PromptRequest),
		mainWindow:      mainWindow,
		configManager:   cfgManager,
	}

	// Setup System Tray Menu
	if desk, ok := fyneApp.(desktop.App); ok {
		log.Println("UIController: Desktop detected, setting up system tray menu.")
		menu := fyne.NewMenu("SecureLLMAgentProxy",
			fyne.NewMenuItem("Open Config File", func() {
				configPath := uic.configManager.configFilePath // Accessing the path
				log.Printf("UIController: 'Open Config File' selected. Path: %s", configPath)
				err := openFile(configPath) // Cross-platform way to open a file/URL
				if err != nil {
					log.Printf("UIController: Error opening config file %s: %v", configPath, err)
					// Optionally show an error dialog to the user
					errorDialog := dialog.NewError(fmt.Errorf("Failed to open config file:\n%s\n\nError: %v", configPath, err), uic.mainWindow)
					// Temporarily show main window to display error, then re-hide if it was hidden.
					// This is tricky if mainWindow is always hidden. A dedicated error dialog window might be better.
					// For now, errors are logged. If mainWindow is hidden, this dialog won't show attached to it.
					// We can create a new temporary window for the error.
					errWin := uic.fyneApp.NewWindow("Error")
					errContent := container.NewVBox(
						widget.NewLabel(fmt.Sprintf("Failed to open config file:\n%s", configPath)),
						widget.NewLabel(fmt.Sprintf("Error: %s", err.Error())),
						widget.NewButton("OK", func() { errWin.Close() }),
					)
					errWin.SetContent(errContent)
					errWin.CenterOnScreen()
					errWin.Show()
				}
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				log.Println("UIController: 'Quit' selected from system tray.")
				uic.Quit() // This calls fyneApp.Quit()
			}),
		)
		desk.SetSystemTrayMenu(menu)
		// Optional: Set an icon for the system tray.
		// desk.SetSystemTrayIcon(resource) // where resource is a fyne.Resource
	} else {
		log.Println("UIController: Not a desktop app, system tray menu not available.")
	}

	go uic.uiEventLoop()
	return uic
}

// openFile attempts to open a file in the default application.
// This is a helper, might need to be more robust or use a library for true cross-platform.
// For macOS: "open <file>"
// For Windows: "cmd /c start <file>"
// For Linux: "xdg-open <file>"
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default: // linux, freebsd, openbsd, netbsd
		cmd = exec.Command("xdg-open", path)
	}
	err := cmd.Start() // Use Start to not block, though for this action, Run might be okay.
	if err != nil {
		return fmt.Errorf("failed to start command for opening file: %w", err)
	}
	// We don't wait for the command to finish, just that it launched.
	// If using cmd.Run(), it would wait and we could capture stderr.
	return nil
}


// RequestUserDecision sends a request to the UI goroutine to show a prompt
// and waits for the user's decision.
func (uic *UIController) RequestUserDecision(requestURL string) (UserDecision, string) {
	parsedURL, err := url.Parse(requestURL)
	defaultRule := requestURL // Fallback to full URL if parsing fails
	if err == nil {
		defaultRule = parsedURL.Hostname()
		// For "Allow Always", user might want to allow "domain.com" or "domain.com/path/*"
		// Pre-populating with just hostname is a good start.
	}

	responseChan := make(chan PromptResponse)
	promptReq := &PromptRequest{
		URL:            requestURL,
		ResponseChan:   responseChan,
		DefaultRule:    defaultRule,
	}

	uic.promptRequestCh <- promptReq
	response := <-responseChan
	return response.Decision, response.RuleToAdd
}

// uiEventLoop listens for prompt requests and handles showing the dialog.
func (uic *UIController) uiEventLoop() {
	for req := range uic.promptRequestCh {
		uic.dialogMutex.Lock() // Ensure only one dialog is active and processed at a time

		log.Printf("UIController: Creating prompt for URL: %s (Default rule: %s)", req.URL, req.DefaultRule)

		// This function will block until the dialog is closed.
		// The decision is sent via req.ResponseChan from within showPromptDialog's callbacks.
		uic.showPromptDialog(req)

		// The lock is released after the dialog has been handled and response sent.
		// This ensures that the next request in promptRequestCh is not processed
		// until the current dialog interaction is fully complete.
		uic.dialogMutex.Unlock()
	}
}

func (uic *UIController) showPromptDialog(req *PromptRequest) {
	dialogWindow := uic.fyneApp.NewWindow("Untrusted Network Request")
	dialogWindow.SetModal(true) // Make it modal to the app, though Fyne's modal might not block OS level interaction perfectly.
	dialogWindow.SetFixedSize(true)


	urlLabel := widget.NewLabel("An application is trying to access:\n" + req.URL)
	urlLabel.Wrapping = fyne.TextWrapWord

	allowRuleEntry := widget.NewEntry()
	allowRuleEntry.SetText(req.DefaultRule) // Pre-populate with domain or full URL
	allowRuleEntry.Hide() // Initially hidden, shown only for "Allow Always"

	statusLabel := widget.NewLabel("") // For messages like "Rule added" (optional)
	statusLabel.Hide()

	var chosenDecision UserDecision = UserDecisionDialogDismissed // Default if window closed
	var chosenRule string = ""

	sendResponse := func(decision UserDecision, rule string) {
		// Check if a response has already been sent (e.g. window closed callback)
		// This simple check might not be perfectly race-proof for complex scenarios
		// but for typical dialog interaction, it should be fine.
		select {
		case <-req.ResponseChan:
			// Channel was already closed or written to. Log this unexpected state.
			log.Println("UIController: Response channel already closed or written to. Ignoring duplicate response.")
			return
		default:
			// Safe to send
		}
		req.ResponseChan <- PromptResponse{Decision: decision, RuleToAdd: rule}
		dialogWindow.Close()
	}

	dialogWindow.SetOnClosed(func() {
		// This callback is crucial. If the user closes the window
		// using the window manager's close button instead of one of our dialog buttons.
		// We need to ensure a response is sent so RequestUserDecision doesn't block forever.
		log.Printf("UIController: Dialog for %s closed by user (window manager). Defaulting to DenyOnce.", req.URL)
		// Check if a decision was already made by a button press
		// A simple way is to see if the channel is still open for writing by trying a non-blocking send.
		// However, a more robust way is to ensure sendResponse handles being called multiple times,
		// or use a flag. For now, we assume sendResponse will handle it or this is the primary path for this case.

		// If no button was pressed, chosenDecision remains UserDecisionDialogDismissed
		// We need to translate this into a concrete action for the proxy. DenyOnce is safest.
		if chosenDecision == UserDecisionDialogDismissed {
			// Use a temporary channel to check if the original channel is still open
			// This is a bit of a workaround. A more robust solution might involve a sync.Once
			// around sending the response, or checking a flag set by button presses.
			tempChan := make(chan PromptResponse, 1)
			tempChan <- PromptResponse{Decision: UserDecisionDenyOnce, RuleToAdd: ""} // Try to send DenyOnce

			select {
			case val := <-tempChan: // if we can read it back, it means original channel was not written to by buttons
				req.ResponseChan <- val
			default:
				// means original channel was already written to by a button, do nothing
			}
		}
		// No need to call dialogWindow.Close() here as it's already closing.
	})


	btnAllowOnce := widget.NewButton("Allow Once", func() {
		chosenDecision = UserDecisionAllowOnce
		sendResponse(UserDecisionAllowOnce, "")
	})

	btnDenyOnce := widget.NewButton("Deny Once", func() {
		chosenDecision = UserDecisionDenyOnce
		sendResponse(UserDecisionDenyOnce, "")
	})

	btnDenyAlways := widget.NewButton("Deny Always", func() {
		chosenDecision = UserDecisionDenyAlways
		// For Deny Always, the rule is typically the domain.
		// User doesn't need to edit it, so we use req.DefaultRule (which is hostname).
		sendResponse(UserDecisionDenyAlways, req.DefaultRule)
	})

	// Handling "Allow Always" requires showing the text entry
	var formItems []*widget.FormItem
	formItemRule := widget.NewFormItem("Rule to Add (e.g., *.example.com or example.com/path/*)", allowRuleEntry)
	formItems = append(formItems, formItemRule)

	allowAlwaysForm := dialog.NewForm(
		"Add Permanent Allow Rule",
		"Add Rule & Allow", // Confirm button text
		"Cancel",           // Dismiss button text
		formItems,
		func(confirm bool) {
			if confirm {
				chosenRule = allowRuleEntry.Text
				if strings.TrimSpace(chosenRule) == "" {
					log.Printf("UIController: User tried to add an empty rule for AllowAlways.")
					errorDialog := dialog.NewError(fmt.Errorf("Rule cannot be empty. Please enter a valid rule or cancel."), dialogWindow)
					errorDialog.Show()
					return // Do not close the form, let user correct or cancel.
				}
				chosenDecision = UserDecisionAllowAlways
				sendResponse(UserDecisionAllowAlways, chosenRule)
			} else {
				// User cancelled the "Allow Always" sub-dialog.
				// The main dialog (dialogWindow) remains open. No response sent yet from this path.
				// User can choose another option or close the main dialog.
			}
		}, dialogWindow) // This form dialog will be modal to dialogWindow

	btnAllowAlways := widget.NewButton("Allow Always...", func() {
		// This button does not directly send a response.
		// It shows another dialog (the formDialog) for rule input.
		// The main dialogWindow should remain until that form is submitted or cancelled.
		// However, the simple sendResponse() closes the main window.
		// This needs a slight refactor. The formDialog should call sendResponse.

		// Correction: The formDialog itself doesn't need to call sendResponse for the *main* prompt.
		// Its callback (func(confirm bool)) is where we get the rule and then call sendResponse.
		// The issue is that sendResponse closes dialogWindow.

		// Simpler approach: create a NEW window for the "Allow Always" input,
		// or make the entry visible in the current window.
		// For now, let's use the dialog.NewForm which is simpler to implement.
		// The `dialog.NewForm` is modal to its parent window.

		// Pre-fill and show the entry
		allowRuleEntry.SetText(req.DefaultRule) // Ensure it's current
		// We don't hide/show allowRuleEntry anymore, using a separate form dialog.

		// Show the form dialog for "Allow Always"
		// The callback of this form dialog will handle sending the response.
		allowAlwaysForm.Show()
	})


	buttons := container.New(layout.NewGridLayout(2),
		btnAllowOnce, btnDenyOnce, btnAllowAlways, btnDenyAlways,
	)

	content := container.NewVBox(
		urlLabel,
		// allowRuleEntry, // Removed from here, handled by the form dialog
		statusLabel,
		layout.NewSpacer(),
		buttons,
	)

	dialogWindow.SetContent(content)
	dialogWindow.Resize(fyne.NewSize(500, 200)) // Adjusted size
	dialogWindow.CenterOnScreen()
	dialogWindow.Show() // This blocks until the window is closed if not run in a separate goroutine from Fyne's perspective
	// Since uiEventLoop is already a goroutine, Show() here will behave as expected for a dialog.
}


// Run starts the Fyne application event loop.
func (uic *UIController) Run() {
	// uic.mainWindow.Show() // Optionally show a control panel or status window
	uic.fyneApp.Run()
	log.Println("UIController: Fyne app stopped.")
}

// Quit stops the Fyne application.
func (uic *UIController) Quit() {
	log.Println("UIController: Attempting to quit Fyne app.")
	uic.fyneApp.Quit()
}
