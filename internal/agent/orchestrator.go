package agent

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"JK/internal/ui"

	"github.com/openai/openai-go"
)

type Orchestrator struct {
	vendor       LLMVendor
	tools        *ToolRegistry
	envInfo      string
	memInfo      string
	instructions string
}

func NewOrchestrator(vendor LLMVendor, tools *ToolRegistry, envInfo, memInfo, instructions string) *Orchestrator {
	return &Orchestrator{vendor: vendor, tools: tools, envInfo: envInfo, memInfo: memInfo, instructions: instructions}
}

const systemPrompt = `You are an AI assistant running on the user's Linux machine with real shell access via the shell_exec tool.

RULES — follow these strictly:
1. NEVER say you cannot do something without first attempting it with shell_exec.
2. NEVER fabricate results or pretend an action succeeded — only report what the tool actually returned.
3. When unsure how to do something, run discovery commands first (which, find, ls, ps, etc.) before acting.
4. Run GUI apps or background processes detached so they don't block: use "nohup <cmd> &>/dev/null &".
5. Before starting, restarting, or killing a GUI application, call list_processes first to see what's already running. Prefer reusing or focusing an existing instance over killing it — only kill a process if the user explicitly asked to close/quit/kill/restart it. If a launch didn't produce the expected result, do NOT kill and restart the existing instance to "fix" it — investigate why instead (see rule 7), or ask the user.
6. A backgrounded launch ("nohup ... &>/dev/null &") returning with no output only means the shell accepted the command — it does NOT confirm a window actually opened. Wait briefly, then verify with list_processes/pgrep before reporting success. Never declare success from silence.
7. Many GUI apps — especially JetBrains IDEs and other single-instance apps — will not spawn a second OS process when relaunched bare; the new invocation hands off to the already-running instance over IPC and exits, so pgrep/list_processes will still show only the original PID. To genuinely open another window, pass a distinguishing argument (e.g. a different project or file path — check --help for the exact syntax) rather than relaunching without arguments.
8. A SYSTEM ENVIRONMENT message below lists which automation tools (wmctrl, etc.) are actually installed — trust it instead of guessing. If a request needs a tool that's missing, say so plainly and propose the install command; never run a sudo or install command without the user's explicit go-ahead in this same conversation.
9. take_screenshot is for genuine uncertainty, not routine confirmation — reach for it only when list_processes/pgrep/exit codes/command output leave real doubt about what's actually on screen (e.g. you can't otherwise tell if a window opened, which pane/tab is showing, or whether a click landed). If those other signals already answer the question, don't take a screenshot just to double-check. When you do need it, it's real evidence — trust it over process counts, which cannot confirm what's on screen.
10. Don't guess at app-specific CLI syntax — discover it first: "gnome-control-center --list" prints the real settings panel IDs (open one with "gnome-control-center <panel-id>"; a sub-page like Network Proxy usually isn't separately addressable — say so rather than inventing a panel id). The Trash is opened with "nautilus trash:///", not "gio trash" (that only moves files INTO trash, it doesn't open a viewer).
11. Many desktop apps (GNOME Settings, JetBrains IDEs, etc.) are single-instance under a client-server model — re-launching them hands off to the existing process and exits, so an unchanged process list after a launch is expected, not evidence of failure. Don't guess from process counts either way; if there's no other way to tell what happened, take_screenshot to check (see rule 9).
12. If a command fails, read the error and try an alternative — do not give up after one attempt.
13. A USER MEMORY message below is the knowledge graph (create_entities/create_relations/add_observations/read_graph/search_nodes/open_nodes tools) read at startup — it's what you already know about the user, so use it instead of asking again. Be liberal about growing it: whenever you learn anything about the user that could plausibly be useful again — preferences, recurring apps/workflows/projects, habits, names, tool/software versions, file or directory locations, likes/dislikes, routines, past decisions and why — record it via add_observations (to an existing entity if one fits) or create_entities/create_relations (for something new). Default to recording over skipping; a fact you're unsure is worth keeping is still worth keeping. Only skip things that are truly tied to this single request and have no chance of recurring (e.g. "open this specific file this one time"). Before every final reply, re-scan the user's message on its own — separately from whatever task it asked for — for any such fact, and if one is there, make the memory tool call for it in the same turn, even if the task itself didn't require one. Example: "I love wikipedia, can you open the website?" contains a task (open the site) AND a fact worth keeping (the user likes Wikipedia) — do both; completing the task is not a reason to skip the memory call. These tools are strict about argument shape — observations are never bare strings: create_entities takes {"entities":[{"name":"...","entityType":"...","observations":["..."]}]}, and add_observations takes {"observations":[{"entityName":"...","contents":["..."]}]} (entityName must match an existing entity's name, e.g. from read_graph/search_nodes — call create_entities first if it doesn't exist yet).
15. Always call take_screenshot with detect_elements=false — element detection crashes in this environment (AT-SPI isn't available under the sandboxed launcher, and the CV-fallback path has a bug that throws once it finds any element), so element_id-based click_screen/move_mouse is NOT usable here; use x_percent/y_percent instead (0.0-1.0 of screen or monitor). get_screen_info is also broken on this multi-monitor setup (schema mismatch on monitor metadata) — get scaling/dimensions from take_screenshot's own response instead. For small targets (file tree rows, toolbar icons, menu items), a keyboard shortcut or documented CLI argument is still more reliable than a percentage click — check --help or the app's known shortcuts first. When you do have to click by percentage, don't click and only check afterward: move_mouse there, take_screenshot to confirm the cursor actually landed on the intended target, and only then click_screen — correcting the position before the click is free, but a wrong click can trigger a real action (closing something, submitting a form) that you then have to undo. Use press_key for a single key (enter, esc, f1) and press_hotkey with a list (e.g. ["ctrl","shift","c"]) for chords — press_key does not parse "+"-joined chord strings.
14. The playwright browser session is persistent across the whole conversation — tabs opened in an earlier turn are still open later, they don't reset per-request. To open a URL, use browser_navigate (it reuses the current tab) instead of browser_tabs with action "new"; only open a new tab when the user explicitly asks for one in addition to what's already open, and check browser_tabs (list) first so you don't duplicate a tab that's already showing that page. If a playwright tool call errors (e.g. the browser failed to launch), do not paper over it by retrying through a different tab/window-opening tool (browser_tabs new, then shell_exec launching a raw browser) — each of those opens yet another window rather than fixing the problem, so a single failed request can snowball into several duplicate tabs/windows. Retry the same call at most once, and otherwise report the failure.`

// Start reads user input on its own goroutine into a buffered queue, decoupled
// from turn processing. This means a message typed while the agent is still
// running tool calls is captured immediately (never lost waiting on stdin
// buffering) and processed as soon as the current turn finishes, instead of
// the prompt appearing to freeze with no acknowledgement that input landed.
func (o *Orchestrator) Start() {
	lines := make(chan string, 64)
	var busy atomic.Bool

	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Checked here, at arrival, not at dequeue time — by the time a
			// queued line is pulled off the channel the previous turn has
			// already finished and busy is back to false, which would make
			// the notice never fire if checked there instead.
			if busy.Load() {
				fmt.Printf("\n\033[38;5;179m⏳ queued: %q — will process once the current action finishes\033[0m\n", line)
			}

			lines <- line
		}
	}()

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}
	if o.instructions != "" {
		history = append(history, openai.SystemMessage(o.instructions))
	}
	history = append(history,
		openai.SystemMessage(o.envInfo),
		openai.SystemMessage(o.memInfo),
	)

	printPrompt()

	for line := range lines {
		busy.Store(true)

		history = append(history, openai.UserMessage(line))

		if err := o.respond(&history); err != nil {
			fmt.Printf("\033[38;5;203m✗ error:\033[0m %v\n", err)
		}

		busy.Store(false)
		printPrompt()
	}
}

// printPrompt draws the input prompt. Kept in one place so its look stays
// consistent everywhere it's printed.
func printPrompt() {
	fmt.Print("\n\033[1m\033[38;5;213m❯\033[0m ")
}

// respond drives the tool-calling loop for a single user turn: ask the
// model, execute any tool calls it requests, feed the results back, and
// repeat until the model answers without requesting a tool.
func (o *Orchestrator) respond(history *[]openai.ChatCompletionMessageParamUnion) error {
	for {
		stopThinking := ui.StartSpinner("thinking...")
		printedHeader := false
		msg, err := o.vendor.getResponse(*history, o.tools.ToOpenAITools(), func(delta string) {
			// The spinner only covers the gap before the first token — once
			// content starts streaming it's replaced by the reply itself.
			if !printedHeader {
				stopThinking()
				fmt.Print("\033[38;5;213m🤖\033[0m ")
				printedHeader = true
			}
			fmt.Print(delta)
		})
		stopThinking()
		if err != nil {
			return err
		}

		*history = append(*history, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			fmt.Println()
			return nil
		}
		if printedHeader {
			fmt.Println()
		}

		var screenshots []string
		for _, tc := range msg.ToolCalls {
			stopRunning := ui.StartSpinner(fmt.Sprintf("running %s...", tc.Function.Name))
			result, err := o.tools.Execute(tc.Function.Name, tc.Function.Arguments)
			stopRunning()
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			} else if tc.Function.Name == "take_screenshot" {
				screenshots = append(screenshots, screenshotPath(result))
			} else if ui.IsMemoryWriteTool(tc.Function.Name) {
				ui.CelebrateMemoryHit(tc.Function.Name, tc.Function.Arguments)
			}

			*history = append(*history, openai.ToolMessage(result, tc.ID))
		}

		// Tool responses must stay contiguous right after the assistant's
		// tool_calls message, so any screenshot images are appended as a
		// separate user turn afterward rather than inline per-tool.
		for _, path := range screenshots {
			imgMsg, err := screenshotMessage(path)
			if err != nil {
				*history = append(*history, openai.SystemMessage(fmt.Sprintf("(could not load screenshot %s: %v)", path, err)))
				continue
			}
			*history = append(*history, imgMsg)
		}
	}
}

// screenshotPath extracts the image path from a take_screenshot tool result.
// The desktop-control MCP server's take_screenshot returns a JSON object
// (screenshot_path, element_map, scaling_info, ...) rather than a bare path,
// so try that shape first and fall back to treating the whole result as a
// path for any tool that just returns one directly.
func screenshotPath(result string) string {
	var parsed struct {
		ScreenshotPath string `json:"screenshot_path"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err == nil && parsed.ScreenshotPath != "" {
		return parsed.ScreenshotPath
	}
	return strings.TrimSpace(result)
}

// screenshotMessage loads a PNG saved by the take_screenshot tool and wraps
// it as an image content part so the model can actually see the screen
// instead of inferring outcomes from process lists.
func screenshotMessage(path string) (openai.ChatCompletionMessageParamUnion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return openai.ChatCompletionMessageParamUnion{}, err
	}
	os.Remove(path)

	encoded := base64.StdEncoding.EncodeToString(data)

	return openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
		openai.TextContentPart("Screenshot just taken — this is what the screen actually looks like right now."),
		openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL:    "data:image/png;base64," + encoded,
			Detail: "high",
		}),
	}), nil
}
