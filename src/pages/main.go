package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand" // Imported to resolve undefined: rand
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	chromelog "github.com/chromedp/cdproto/log" // Aliased to prevent naming conflict
	"github.com/chromedp/chromedp"
	stdLog "log"
)

const (
	CHROME_WINDOW_WIDTH     = 1200
	CHROME_WINDOW_HEIGHT    = 800
	CONFIG_DIR_NAME         = ".bidding-bot"
	CONFIG_FILE_NAME        = "config.json"
	SYSFILES_FOLDER         = "sysfiles"
	CHROME_USER_DATA_DIR    = "chrome_user_data"
	DOWNLOADS_FOLDER        = "downloads"
	USERFILES_FOLDER        = "userfiles"
	LOG_FILE_NAME           = "bot_debug.log"
	DEBUG_LOGGING_ENABLED   = true // Set to true to enable detailed debug logs
	DEFAULT_THREAD_COUNT    = 3
	DEFAULT_MIN_DEADLINE_HS = 0
	DEFAULT_MAX_DEADLINE_HS = 2880
)

var (
	stopFlag          int32
	orderToThreadMap  = make(map[string]int)
	orderLock         sync.Mutex
	executorWG        sync.WaitGroup
	mainCtx, mainCancel = context.WithCancel(context.Background())

	cfg = &Config{}

	userEmail    string
	userPassword string

	debugLogger *stdLog.Logger
)

// Config stores settings other than credentials.
type Config struct {
	MessageEnabled     bool   `json:"message_enabled"`
	MessageText        string `json:"message_text"`
	DiscardAssignments bool   `json:"discard_assignments"`
	DiscardEditing     bool   `json:"discard_editing"`
	MinDeadlineHours   int    `json:"min_deadline_hours"`
	MaxDeadlineHours   int    `json:"max_deadline_hours"`
	ThreadCount        int    `json:"thread_count"`
}

func init() {
	// Initialize debug logger
	if DEBUG_LOGGING_ENABLED {
		logFile, err := os.OpenFile(LOG_FILE_NAME, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			stdLog.Fatalf("Failed to open log file: %v", err)
		}
		debugLogger = stdLog.New(logFile, "DEBUG: ", stdLog.Ldate|stdLog.Ltime|stdLog.Lshortfile)
	} else {
		debugLogger = stdLog.New(ioutil.Discard, "", 0)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	ensureFolders()
	loadConfig()

	// Initialize Chromedp with existing Chrome
	// Attempt to find Chrome executable path based on OS
	chromePath, err := findChromeExecutable()
	if err != nil {
		stdLog.Fatalf("Failed to find Chrome executable: %v", err)
	}

	// Create Chromedp allocator with anti-detection measures
	allocCtx, cancel := chromedp.NewExecAllocator(mainCtx, append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("start-maximized", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
	)...)
	defer cancel()

	a := app.New()
	w := a.NewWindow("Bidding Bot (Go Version)")
	w.Resize(fyne.NewSize(400, 500))

	// HOME UI
	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("Email")
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")

	startStopButton := widget.NewButton("Start", nil)

	homeContent := container.NewVBox(
		widget.NewLabel("Email:"), emailEntry,
		widget.NewLabel("Password:"), passwordEntry,
		startStopButton,
	)

	// SETTINGS UI
	messageCheck := widget.NewCheck("Chat Message", func(v bool) {})
	messageCheck.SetChecked(cfg.MessageEnabled)

	messageArea := widget.NewMultiLineEntry()
	messageArea.SetText(cfg.MessageText)

	threadEntry := widget.NewEntry()
	threadEntry.SetText(strconv.Itoa(cfg.ThreadCount))

	discardAssignmentsCheck := widget.NewCheck("Discard Assignments", func(v bool) {})
	discardAssignmentsCheck.SetChecked(cfg.DiscardAssignments)

	discardEditingCheck := widget.NewCheck("Discard Editing", func(v bool) {})
	discardEditingCheck.SetChecked(cfg.DiscardEditing)

	minDeadlineEntry := widget.NewEntry()
	minDeadlineEntry.SetText(strconv.Itoa(cfg.MinDeadlineHours))

	maxDeadlineEntry := widget.NewEntry()
	maxDeadlineEntry.SetText(strconv.Itoa(cfg.MaxDeadlineHours))

	saveSettingsButton := widget.NewButton("Save Settings", func() {
		cfg.MessageEnabled = messageCheck.Checked
		cfg.MessageText = messageArea.Text
		cfg.DiscardAssignments = discardAssignmentsCheck.Checked
		cfg.DiscardEditing = discardEditingCheck.Checked
		minDH, err1 := strconv.Atoi(minDeadlineEntry.Text)
		maxDH, err2 := strconv.Atoi(maxDeadlineEntry.Text)
		tc, err3 := strconv.Atoi(threadEntry.Text)

		if err1 != nil || err2 != nil || err3 != nil {
			dialog.ShowError(fmt.Errorf("invalid numeric input in settings"), w)
			return
		}

		cfg.MinDeadlineHours = minDH
		cfg.MaxDeadlineHours = maxDH
		if tc <= 0 {
			tc = DEFAULT_THREAD_COUNT
		}
		cfg.ThreadCount = tc
		saveConfig()
		dialog.ShowInformation("Settings Saved", "Your settings have been saved.", w)
	})

	settingsContent := container.NewVBox(
		messageCheck,
		messageArea,
		widget.NewLabel("Threads:"), threadEntry,
		discardAssignmentsCheck,
		discardEditingCheck,
		widget.NewLabel("Minimum Deadline (hours):"), minDeadlineEntry,
		widget.NewLabel("Maximum Deadline (hours):"), maxDeadlineEntry,
		saveSettingsButton,
	)

	currentContent := container.NewVBox(homeContent)

	// Menu Setup
	homeItem := fyne.NewMenuItem("Home", func() {
		currentContent.Objects = []fyne.CanvasObject{homeContent}
		currentContent.Refresh()
	})
	settingsItem := fyne.NewMenuItem("Settings", func() {
		currentContent.Objects = []fyne.CanvasObject{settingsContent}
		currentContent.Refresh()
	})
	menu := fyne.NewMainMenu(
		fyne.NewMenu("Menu", homeItem, settingsItem),
	)
	w.SetMainMenu(menu)

	var running bool
	startStopButton.SetText("Start")
	startStopButton.OnTapped = func() {
		if !running {
			userEmail = emailEntry.Text
			userPassword = passwordEntry.Text

			if userEmail == "" || userPassword == "" {
				dialog.ShowError(fmt.Errorf("please enter both email and password"), w)
				return
			}

			atomic.StoreInt32(&stopFlag, 0)
			startBot(allocCtx, w, chromePath)
			startStopButton.SetText("Stop")
			running = true
		} else {
			stopBot()
			startStopButton.SetText("Start")
			running = false
		}
	}

	w.SetContent(currentContent)
	w.ShowAndRun()
}

func startBot(allocCtx context.Context, win fyne.Window, chromePath string) {
	stdLog.Println("Starting the bidding bot...")
	debugLogger.Println("Bot start initiated.")

	executorWG = sync.WaitGroup{}
	for i := 0; i < cfg.ThreadCount; i++ {
		executorWG.Add(1)
		go runWorker(i, allocCtx)
	}

	dialog.ShowInformation("Bot Started", "The bidding bot has started working.", win)
}

func stopBot() {
	atomic.StoreInt32(&stopFlag, 1)
	stdLog.Println("Stop signal issued.")
	debugLogger.Println("Stop signal has been set.")

	// Wait for all workers to finish
	executorWG.Wait()

	// Cancel the main context to close Chrome instances
	mainCancel()
	stdLog.Println("Bidding bot stopped.")
	debugLogger.Println("Main context canceled, Chrome instances should close.")
}

func runWorker(threadIndex int, allocCtx context.Context) {
	defer executorWG.Done()
	debugLogger.Printf("Worker %d started.", threadIndex)

	userDataDir := filepath.Join(getSysfilesDir(), CHROME_USER_DATA_DIR, strconv.Itoa(threadIndex))
	os.MkdirAll(userDataDir, 0755)

	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Create a new context with the user data directory
	taskCtx, cancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(debugLogger.Printf),
	)
	defer cancel()

	// Configure Chrome options for anti-detection
	opts := []chromedp.RunBrowserOption{
		chromedp.WindowSize(CHROME_WINDOW_WIDTH, CHROME_WINDOW_HEIGHT),
		chromedp.UserAgent(userAgent),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
	}

	// Apply options
	err := chromedp.Run(taskCtx, opts...)
	if err != nil {
		stdLog.Printf("Worker %d: Failed to run Chromedp with options: %v", threadIndex, err)
		debugLogger.Printf("Worker %d: Chromedp run error: %v", threadIndex, err)
		return
	}

	// Enable verbose logging for chromedp
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *chromelog.EventEntryAdded:
			debugLogger.Printf("Chromedp Log: %s", ev.Entry.Text)
		}
	})

	// Check if session is valid
	sessionValid := checkSession(taskCtx)
	if sessionValid {
		stdLog.Printf("Thread %d: Existing session found, no login required.", threadIndex)
		debugLogger.Printf("Thread %d: Existing session confirmed.", threadIndex)
	} else {
		stdLog.Printf("Thread %d: No valid session found, attempting to log in.", threadIndex)
		debugLogger.Printf("Thread %d: Session invalid, performing login.", threadIndex)
		if err := performLogin(taskCtx, userEmail, userPassword); err != nil {
			stdLog.Printf("Thread %d: Failed to login: %v", threadIndex, err)
			debugLogger.Printf("Thread %d: Login error: %v", threadIndex, err)
			return
		}
		stdLog.Printf("Thread %d: Logged in successfully.", threadIndex)
		debugLogger.Printf("Thread %d: Login successful.", threadIndex)
	}

	// Initial delay after login/session check (3-7 seconds)
	initialWait := time.Duration(rand.Intn(4000)+3000) * time.Millisecond
	stdLog.Printf("Thread %d: Initial wait for %v before starting to bid.", threadIndex, initialWait)
	debugLogger.Printf("Thread %d: Sleeping for %v before bidding loop.", threadIndex, initialWait)
	time.Sleep(initialWait)

	// Main bidding loop
	for atomic.LoadInt32(&stopFlag) == 0 {
		processed, err := findAndHandleSingleOrder(taskCtx, threadIndex)
		if atomic.LoadInt32(&stopFlag) != 0 {
			break
		}
		if err != nil {
			if isContextError(err) {
				stdLog.Printf("Thread %d: No orders processed or context error, continuing.", threadIndex)
				debugLogger.Printf("Thread %d: Context error: %v", threadIndex, err)
				continue
			} else {
				stdLog.Printf("Thread %d: Error processing order: %v", threadIndex, err)
				debugLogger.Printf("Thread %d: Unexpected error: %v", threadIndex, err)
				continue
			}
		}

		if !processed {
			stdLog.Printf("Thread %d: No orders processed, continuing.", threadIndex)
			debugLogger.Printf("Thread %d: No orders found during this iteration.", threadIndex)
			continue
		}

		// No sleep to ensure immediate responsiveness
	}
	debugLogger.Printf("Worker %d exiting loop.", threadIndex)
}

func checkSession(ctx context.Context) bool {
	ctxCheck, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := chromedp.Run(ctxCheck,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		debugLogger.Printf("Session check failed: %v", err)
		return false
	}
	return true
}

func performLogin(ctx context.Context, email, password string) error {
	ctxLogin, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctxLogin,
		chromedp.Navigate("https://essayshark.com/log-in.html"),
		chromedp.WaitVisible(`input[name="login"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="password"]`, chromedp.ByQuery),
		chromedp.Clear(`input[name="login"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="login"]`, email),
		chromedp.Clear(`input[name="password"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="password"]`, password),
		chromedp.Click(`button.bb-button[type="submit"]`, chromedp.NodeVisible),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error during login: %w", err)
	}

	return nil
}

func findAndHandleSingleOrder(ctx context.Context, threadIndex int) (bool, error) {
	ctxOrders, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err := chromedp.Run(ctxOrders,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`tr.order_container`, chromedp.ByQuery),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error navigating to orders page: %v", threadIndex, err)
		return false, err
	}

	var result struct {
		Links     []string `json:"links"`
		Services  []string `json:"services"`
		Deadlines []string `json:"deadlines"`
	}

	err = chromedp.Run(ctxOrders,
		chromedp.Evaluate(`
			(function(){
				let rows = document.querySelectorAll("tr.order_container");
				let data = {links: [], services: [], deadlines: []};
				for (let row of rows) {
					let topicLink = row.querySelector("td.topictitle a");
					let serviceEl = row.querySelector("div.service_type");
					let deadlineEl = row.querySelector("td.td_deadline span.d-deadline + span.d-left");
					data.links.push(topicLink ? topicLink.href : "");
					data.services.push(serviceEl ? serviceEl.textContent.trim() : "");
					data.deadlines.push(deadlineEl ? deadlineEl.textContent.trim() : "");
				}
				return data;
			})()
		`, &result),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error evaluating orders: %v", threadIndex, err)
		return false, err
	}

	orderLinks := result.Links
	serviceTypes := result.Services
	deadlineTexts := result.Deadlines

	for i, orderUrl := range orderLinks {
		if orderUrl == "" {
			continue
		}
		if atomic.LoadInt32(&stopFlag) != 0 {
			return false, nil
		}

		orderLock.Lock()
		_, alreadyProcessing := orderToThreadMap[orderUrl]
		if alreadyProcessing {
			orderLock.Unlock()
			continue
		}
		orderToThreadMap[orderUrl] = threadIndex
		orderLock.Unlock()

		if shouldDiscardServiceType(serviceTypes[i]) {
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		dh := convertDeadlineToHours(deadlineTexts[i])
		if dh != -1 && (dh < cfg.MinDeadlineHours || dh > cfg.MaxDeadlineHours) {
			stdLog.Printf("Thread %d: Order %s deadline (%dh) out of range.", threadIndex, orderUrl, dh)
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Open order details
		ctxOrderDetail, cancelOrderDetail := context.WithTimeout(ctx, 20*time.Second)
		defer cancelOrderDetail()

		err = chromedp.Run(ctxOrderDetail,
			chromedp.Navigate(orderUrl),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
		)
		if err != nil {
			stdLog.Printf("Thread %d: Failed to open order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Handle the order (place bid or apply)
		err = handleOrder(ctxOrderDetail, orderUrl, threadIndex)
		if err != nil {
			stdLog.Printf("Thread %d: Error handling order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Remove from processing map
		orderLock.Lock()
		delete(orderToThreadMap, orderUrl)
		orderLock.Unlock()

		return true, nil // Processed an order
	}

	return false, nil // No orders processed
}

func handleOrder(ctx context.Context, orderUrl string, threadIndex int) error {
	isFixed, err := isFixedPriceOrder(ctx)
	if err != nil {
		return fmt.Errorf("error checking if order is fixed-price: %w", err)
	}

	if hasCountdown, seconds := checkCountdown(ctx); hasCountdown {
		stdLog.Printf("Thread %d: Order %s has countdown: %d seconds. Waiting...", threadIndex, orderUrl, seconds)
		debugLogger.Printf("Thread %d: Waiting for %d seconds due to countdown.", threadIndex, seconds)
		time.Sleep(time.Duration(seconds) * time.Second)
	}

	if hasAttachments(ctx) {
		err := downloadFileIfAvailable(ctx)
		if err != nil {
			stdLog.Printf("Thread %d: Error downloading attachments for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Attachment download error: %v", threadIndex, err)
		}
	}

	if isFixed {
		stdLog.Printf("Thread %d: Order %s is fixed-price. Applying directly.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Applying for fixed-price order.", threadIndex)
		err = applyForOrder(ctx)
		if err != nil {
			return fmt.Errorf("error applying for fixed-price order: %w", err)
		}
	} else {
		stdLog.Printf("Thread %d: Order %s is not fixed-price. Placing bid.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Placing bid on order.", threadIndex)
		err = placeBid(ctx, threadIndex)
		if err != nil {
			return fmt.Errorf("error placing bid: %w", err)
		}
	}

	if cfg.MessageEnabled {
		err = sendMessageToClient(ctx, cfg.MessageText)
		if err != nil {
			stdLog.Printf("Thread %d: Error sending message for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Message sending error: %v", threadIndex, err)
		}
	}

	// Navigate back to orders page without delay
	ctxBack, cancelBack := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBack()
	err = chromedp.Run(ctxBack, chromedp.Navigate("https://essayshark.com/writer/orders/"))
	if err != nil {
		stdLog.Printf("Thread %d: Error navigating back to orders page: %v", threadIndex, err)
		debugLogger.Printf("Thread %d: Navigation back error: %v", threadIndex, err)
	}

	return nil
}

func isFixedPriceOrder(ctx context.Context) (bool, error) {
	var bodyText string
	ctxCheck, cancelCheck := context.WithTimeout(ctx, 10*time.Second)
	defer cancelCheck()

	err := chromedp.Run(ctxCheck, chromedp.Text("body", &bodyText))
	if err != nil {
		return false, fmt.Errorf("error retrieving page body: %w", err)
	}

	if strings.Contains(strings.ToLower(bodyText), "this field is disabled for fixed-price orders") {
		return true, nil
	}
	return false, nil
}

func checkCountdown(ctx context.Context) (bool, int) {
	var countdownText string
	ctxCount, cancelCount := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCount()

	err := chromedp.Run(ctxCount,
		chromedp.Text(`#id_read_timeout_sec`, &countdownText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil || countdownText == "" {
		return false, 0
	}
	var sec int
	fmt.Sscanf(countdownText, "%d", &sec)
	return true, sec
}

func hasAttachments(ctx context.Context) bool {
	var bodyText string
	ctxAttach, cancelAttach := context.WithTimeout(ctx, 5*time.Second)
	defer cancelAttach()

	err := chromedp.Run(ctxAttach, chromedp.Text("body", &bodyText))
	if err != nil {
		debugLogger.Printf("Error checking attachments: %v", err)
		return false
	}
	return strings.Contains(strings.ToLower(bodyText), "uploaded additional materials:")
}

func downloadFileIfAvailable(ctx context.Context) error {
	// Implement actual download logic if needed
	// For now, just log the action
	stdLog.Println("Simulated file download to downloads directory.")
	debugLogger.Println("Simulated file download action.")
	return nil
}

func applyForOrder(ctx context.Context) error {
	ctxApply, cancelApply := context.WithTimeout(ctx, 5*time.Second)
	defer cancelApply()

	err := chromedp.Run(ctxApply,
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error clicking apply button: %w", err)
	}
	return nil
}

func placeBid(ctx context.Context, threadIndex int) error {
	ctxBid, cancelBid := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBid()

	err := chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", "-1.00", chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting bid value or clicking apply: %w", err)
	}

	var errText string
	err = chromedp.Run(ctxBid,
		chromedp.Text("#id_bid4-error", &errText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error retrieving bid error message: %w", err)
	}

	minBid := extractMinimumBid(errText)
	if minBid <= 0 {
		stdLog.Printf("Thread %d: Invalid minimum bid extracted, skipping.", threadIndex)
		debugLogger.Printf("Thread %d: Extracted minimum bid is invalid: %f", threadIndex, minBid)
		return nil
	}

	err = chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", fmt.Sprintf("%.2f", minBid), chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting minimum bid or clicking apply: %w", err)
	}

	return nil
}

func extractMinimumBid(errorMessage string) float64 {
	var amount float64
	fmt.Sscanf(errorMessage, "Minimum bid is $%f", &amount)
	return amount
}

func sendMessageToClient(ctx context.Context, msg string) error {
	ctxMsg, cancelMsg := context.WithTimeout(ctx, 5*time.Second)
	defer cancelMsg()

	err := chromedp.Run(ctxMsg,
		chromedp.SetValue("#id_body", msg, chromedp.ByID),
		chromedp.Click("#id_send_message", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}

	return nil
}

func discardOrder(orderUrl string) {
	stdLog.Printf("Discarding order %s.", orderUrl)
	debugLogger.Printf("Order %s discarded based on service type.", orderUrl)
}

func shouldDiscardServiceType(serviceType string) bool {
	serviceTypeLower := strings.ToLower(serviceType)
	if cfg.DiscardAssignments && serviceTypeLower == "writing help or assignments" {
		return true
	}
	if cfg.DiscardEditing && serviceTypeLower == "editing" {
		return true
	}
	return false
}

func convertDeadlineToHours(deadlineText string) int {
	if deadlineText == "" {
		return -1
	}
	var days, hours int
	_, err := fmt.Sscanf(deadlineText, "%dd %dh", &days, &hours)
	if err != nil {
		return -1
	}
	return days*24 + hours
}

func loadConfig() {
	configPath := getConfigPath()
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		stdLog.Println("Config file not found. Using default settings.")
		debugLogger.Println("Config load error:", err)
		setDefaultConfig()
		return
	}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		stdLog.Println("Error parsing config file. Using default settings.")
		debugLogger.Println("Config parse error:", err)
		setDefaultConfig()
	}
}

func setDefaultConfig() {
	cfg.MessageEnabled = false
	cfg.MessageText = ""
	cfg.DiscardAssignments = false
	cfg.DiscardEditing = false
	cfg.MinDeadlineHours = DEFAULT_MIN_DEADLINE_HS
	cfg.MaxDeadlineHours = DEFAULT_MAX_DEADLINE_HS
	cfg.ThreadCount = DEFAULT_THREAD_COUNT
}

func saveConfig() {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)
	configPath := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		stdLog.Printf("Error marshaling config: %v", err)
		debugLogger.Printf("Config marshal error: %v", err)
		return
	}
	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		stdLog.Printf("Error writing config file: %v", err)
		debugLogger.Printf("Config write error: %v", err)
	}
}

func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, CONFIG_DIR_NAME)
}

func getConfigPath() string {
	return filepath.Join(getConfigDir(), CONFIG_FILE_NAME)
}

func getSysfilesDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return filepath.Join(cwd, SYSFILES_FOLDER)
}

func findChromeExecutable() (string, error) {
	// Attempt to find Chrome executable based on OS
	if isWindows() {
		paths := []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	} else if isMac() {
		p := `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
		if fileExists(p) {
			return p, nil
		}
	} else { // Assume Linux
		paths := []string{
			`/usr/bin/google-chrome`,
			`/usr/bin/chromium-browser`,
			`/usr/bin/chrome`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("Chrome executable not found")
}

func isWindows() bool {
	return strings.Contains(strings.ToLower(runtime.GOOS), "windows")
}

func isMac() bool {
	return strings.Contains(strings.ToLower(runtime.GOOS), "darwin")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func runWorker(threadIndex int, allocCtx context.Context) {
	defer executorWG.Done()
	debugLogger.Printf("Worker %d started.", threadIndex)

	userDataDir := filepath.Join(getSysfilesDir(), CHROME_USER_DATA_DIR, strconv.Itoa(threadIndex))
	os.MkdirAll(userDataDir, 0755)

	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Create a new context with the user data directory
	taskCtx, taskCancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(debugLogger.Printf),
	)
	defer taskCancel()

	// Configure Chrome options for anti-detection
	opts := []chromedp.RunBrowserOption{
		chromedp.WindowSize(CHROME_WINDOW_WIDTH, CHROME_WINDOW_HEIGHT),
		chromedp.UserAgent(userAgent),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.UserDataDir(userDataDir), // Persist session
	}

	// Apply options
	err := chromedp.Run(taskCtx, opts...)
	if err != nil {
		stdLog.Printf("Worker %d: Failed to run Chromedp with options: %v", threadIndex, err)
		debugLogger.Printf("Worker %d: Chromedp run error: %v", threadIndex, err)
		return
	}

	// Enable verbose logging for chromedp
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *chromelog.EventEntryAdded:
			debugLogger.Printf("Chromedp Log: %s", ev.Entry.Text)
		}
	})

	// Check if session is valid
	sessionValid := checkSession(taskCtx)
	if sessionValid {
		stdLog.Printf("Thread %d: Existing session found, no login required.", threadIndex)
		debugLogger.Printf("Thread %d: Existing session confirmed.", threadIndex)
	} else {
		stdLog.Printf("Thread %d: No valid session found, attempting to log in.", threadIndex)
		debugLogger.Printf("Thread %d: Session invalid, performing login.", threadIndex)
		if err := performLogin(taskCtx, userEmail, userPassword); err != nil {
			stdLog.Printf("Thread %d: Failed to login: %v", threadIndex, err)
			debugLogger.Printf("Thread %d: Login error: %v", threadIndex, err)
			return
		}
		stdLog.Printf("Thread %d: Logged in successfully.", threadIndex)
		debugLogger.Printf("Thread %d: Login successful.", threadIndex)
	}

	// Initial delay after login/session check (3-7 seconds)
	initialWait := time.Duration(rand.Intn(4000)+3000) * time.Millisecond
	stdLog.Printf("Thread %d: Initial wait for %v before starting to bid.", threadIndex, initialWait)
	debugLogger.Printf("Thread %d: Sleeping for %v before bidding loop.", threadIndex, initialWait)
	time.Sleep(initialWait)

	// Main bidding loop
	for atomic.LoadInt32(&stopFlag) == 0 {
		processed, err := findAndHandleSingleOrder(taskCtx, threadIndex)
		if atomic.LoadInt32(&stopFlag) != 0 {
			break
		}
		if err != nil {
			if isContextError(err) {
				stdLog.Printf("Thread %d: No orders processed or context error, continuing.", threadIndex)
				debugLogger.Printf("Thread %d: Context error: %v", threadIndex, err)
				continue
			} else {
				stdLog.Printf("Thread %d: Error processing order: %v", threadIndex, err)
				debugLogger.Printf("Thread %d: Unexpected error: %v", threadIndex, err)
				continue
			}
		}

		if !processed {
			stdLog.Printf("Thread %d: No orders processed, continuing.", threadIndex)
			debugLogger.Printf("Thread %d: No orders found during this iteration.", threadIndex)
			continue
		}

		// No sleep to ensure immediate responsiveness
	}
	debugLogger.Printf("Worker %d exiting loop.", threadIndex)
}

func checkSession(ctx context.Context) bool {
	ctxCheck, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := chromedp.Run(ctxCheck,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		debugLogger.Printf("Session check failed: %v", err)
		return false
	}
	return true
}

func performLogin(ctx context.Context, email, password string) error {
	ctxLogin, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctxLogin,
		chromedp.Navigate("https://essayshark.com/log-in.html"),
		chromedp.WaitVisible(`input[name="login"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="password"]`, chromedp.ByQuery),
		chromedp.Clear(`input[name="login"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="login"]`, email),
		chromedp.Clear(`input[name="password"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="password"]`, password),
		chromedp.Click(`button.bb-button[type="submit"]`, chromedp.NodeVisible),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error during login: %w", err)
	}

	return nil
}

func findAndHandleSingleOrder(ctx context.Context, threadIndex int) (bool, error) {
	ctxOrders, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err := chromedp.Run(ctxOrders,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`tr.order_container`, chromedp.ByQuery),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error navigating to orders page: %v", threadIndex, err)
		return false, err
	}

	var result struct {
		Links     []string `json:"links"`
		Services  []string `json:"services"`
		Deadlines []string `json:"deadlines"`
	}

	err = chromedp.Run(ctxOrders,
		chromedp.Evaluate(`
			(function(){
				let rows = document.querySelectorAll("tr.order_container");
				let data = {links: [], services: [], deadlines: []};
				for (let row of rows) {
					let topicLink = row.querySelector("td.topictitle a");
					let serviceEl = row.querySelector("div.service_type");
					let deadlineEl = row.querySelector("td.td_deadline span.d-deadline + span.d-left");
					data.links.push(topicLink ? topicLink.href : "");
					data.services.push(serviceEl ? serviceEl.textContent.trim() : "");
					data.deadlines.push(deadlineEl ? deadlineEl.textContent.trim() : "");
				}
				return data;
			})()
		`, &result),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error evaluating orders: %v", threadIndex, err)
		return false, err
	}

	orderLinks := result.Links
	serviceTypes := result.Services
	deadlineTexts := result.Deadlines

	for i, orderUrl := range orderLinks {
		if orderUrl == "" {
			continue
		}
		if atomic.LoadInt32(&stopFlag) != 0 {
			return false, nil
		}

		orderLock.Lock()
		_, alreadyProcessing := orderToThreadMap[orderUrl]
		if alreadyProcessing {
			orderLock.Unlock()
			continue
		}
		orderToThreadMap[orderUrl] = threadIndex
		orderLock.Unlock()

		if shouldDiscardServiceType(serviceTypes[i]) {
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		dh := convertDeadlineToHours(deadlineTexts[i])
		if dh != -1 && (dh < cfg.MinDeadlineHours || dh > cfg.MaxDeadlineHours) {
			stdLog.Printf("Thread %d: Order %s deadline (%dh) out of range.", threadIndex, orderUrl, dh)
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Open order details
		ctxOrderDetail, cancelOrderDetail := context.WithTimeout(ctx, 20*time.Second)
		defer cancelOrderDetail()

		err = chromedp.Run(ctxOrderDetail,
			chromedp.Navigate(orderUrl),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
		)
		if err != nil {
			stdLog.Printf("Thread %d: Failed to open order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Handle the order (place bid or apply)
		err = handleOrder(ctxOrderDetail, orderUrl, threadIndex)
		if err != nil {
			stdLog.Printf("Thread %d: Error handling order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Remove from processing map
		orderLock.Lock()
		delete(orderToThreadMap, orderUrl)
		orderLock.Unlock()

		return true, nil // Processed an order
	}

	return false, nil // No orders processed
}

func handleOrder(ctx context.Context, orderUrl string, threadIndex int) error {
	isFixed, err := isFixedPriceOrder(ctx)
	if err != nil {
		return fmt.Errorf("error checking if order is fixed-price: %w", err)
	}

	if hasCountdown, seconds := checkCountdown(ctx); hasCountdown {
		stdLog.Printf("Thread %d: Order %s has countdown: %d seconds. Waiting...", threadIndex, orderUrl, seconds)
		debugLogger.Printf("Thread %d: Waiting for %d seconds due to countdown.", threadIndex, seconds)
		time.Sleep(time.Duration(seconds) * time.Second)
	}

	if hasAttachments(ctx) {
		err := downloadFileIfAvailable(ctx)
		if err != nil {
			stdLog.Printf("Thread %d: Error downloading attachments for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Attachment download error: %v", threadIndex, err)
		}
	}

	if isFixed {
		stdLog.Printf("Thread %d: Order %s is fixed-price. Applying directly.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Applying for fixed-price order.", threadIndex)
		err = applyForOrder(ctx)
		if err != nil {
			return fmt.Errorf("error applying for fixed-price order: %w", err)
		}
	} else {
		stdLog.Printf("Thread %d: Order %s is not fixed-price. Placing bid.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Placing bid on order.", threadIndex)
		err = placeBid(ctx, threadIndex)
		if err != nil {
			return fmt.Errorf("error placing bid: %w", err)
		}
	}

	if cfg.MessageEnabled {
		err = sendMessageToClient(ctx, cfg.MessageText)
		if err != nil {
			stdLog.Printf("Thread %d: Error sending message for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Message sending error: %v", threadIndex, err)
		}
	}

	// Navigate back to orders page without delay
	ctxBack, cancelBack := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBack()
	err = chromedp.Run(ctxBack, chromedp.Navigate("https://essayshark.com/writer/orders/"))
	if err != nil {
		stdLog.Printf("Thread %d: Error navigating back to orders page: %v", threadIndex, err)
		debugLogger.Printf("Thread %d: Navigation back error: %v", threadIndex, err)
	}

	return nil
}

func isFixedPriceOrder(ctx context.Context) (bool, error) {
	var bodyText string
	ctxCheck, cancelCheck := context.WithTimeout(ctx, 10*time.Second)
	defer cancelCheck()

	err := chromedp.Run(ctxCheck, chromedp.Text("body", &bodyText))
	if err != nil {
		return false, fmt.Errorf("error retrieving page body: %w", err)
	}

	if strings.Contains(strings.ToLower(bodyText), "this field is disabled for fixed-price orders") {
		return true, nil
	}
	return false, nil
}

func checkCountdown(ctx context.Context) (bool, int) {
	var countdownText string
	ctxCount, cancelCount := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCount()

	err := chromedp.Run(ctxCount,
		chromedp.Text(`#id_read_timeout_sec`, &countdownText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil || countdownText == "" {
		return false, 0
	}
	var sec int
	fmt.Sscanf(countdownText, "%d", &sec)
	return true, sec
}

func hasAttachments(ctx context.Context) bool {
	var bodyText string
	ctxAttach, cancelAttach := context.WithTimeout(ctx, 5*time.Second)
	defer cancelAttach()

	err := chromedp.Run(ctxAttach, chromedp.Text("body", &bodyText))
	if err != nil {
		debugLogger.Printf("Error checking attachments: %v", err)
		return false
	}
	return strings.Contains(strings.ToLower(bodyText), "uploaded additional materials:")
}

func downloadFileIfAvailable(ctx context.Context) error {
	// Implement actual download logic if needed
	// For now, just log the action
	stdLog.Println("Simulated file download to downloads directory.")
	debugLogger.Println("Simulated file download action.")
	return nil
}

func applyForOrder(ctx context.Context) error {
	ctxApply, cancelApply := context.WithTimeout(ctx, 5*time.Second)
	defer cancelApply()

	err := chromedp.Run(ctxApply,
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error clicking apply button: %w", err)
	}
	return nil
}

func placeBid(ctx context.Context, threadIndex int) error {
	ctxBid, cancelBid := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBid()

	err := chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", "-1.00", chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting bid value or clicking apply: %w", err)
	}

	var errText string
	err = chromedp.Run(ctxBid,
		chromedp.Text("#id_bid4-error", &errText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error retrieving bid error message: %w", err)
	}

	minBid := extractMinimumBid(errText)
	if minBid <= 0 {
		stdLog.Printf("Thread %d: Invalid minimum bid extracted, skipping.", threadIndex)
		debugLogger.Printf("Thread %d: Extracted minimum bid is invalid: %f", threadIndex, minBid)
		return nil
	}

	err = chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", fmt.Sprintf("%.2f", minBid), chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting minimum bid or clicking apply: %w", err)
	}

	return nil
}

func extractMinimumBid(errorMessage string) float64 {
	var amount float64
	fmt.Sscanf(errorMessage, "Minimum bid is $%f", &amount)
	return amount
}

func sendMessageToClient(ctx context.Context, msg string) error {
	ctxMsg, cancelMsg := context.WithTimeout(ctx, 5*time.Second)
	defer cancelMsg()

	err := chromedp.Run(ctxMsg,
		chromedp.SetValue("#id_body", msg, chromedp.ByID),
		chromedp.Click("#id_send_message", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}

	return nil
}

func discardOrder(orderUrl string) {
	stdLog.Printf("Discarding order %s.", orderUrl)
	debugLogger.Printf("Order %s discarded based on service type.", orderUrl)
}

func shouldDiscardServiceType(serviceType string) bool {
	serviceTypeLower := strings.ToLower(serviceType)
	if cfg.DiscardAssignments && serviceTypeLower == "writing help or assignments" {
		return true
	}
	if cfg.DiscardEditing && serviceTypeLower == "editing" {
		return true
	}
	return false
}

func convertDeadlineToHours(deadlineText string) int {
	if deadlineText == "" {
		return -1
	}
	var days, hours int
	_, err := fmt.Sscanf(deadlineText, "%dd %dh", &days, &hours)
	if err != nil {
		return -1
	}
	return days*24 + hours
}

func loadConfig() {
	configPath := getConfigPath()
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		stdLog.Println("Config file not found. Using default settings.")
		debugLogger.Println("Config load error:", err)
		setDefaultConfig()
		return
	}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		stdLog.Println("Error parsing config file. Using default settings.")
		debugLogger.Println("Config parse error:", err)
		setDefaultConfig()
	}
}

func setDefaultConfig() {
	cfg.MessageEnabled = false
	cfg.MessageText = ""
	cfg.DiscardAssignments = false
	cfg.DiscardEditing = false
	cfg.MinDeadlineHours = DEFAULT_MIN_DEADLINE_HS
	cfg.MaxDeadlineHours = DEFAULT_MAX_DEADLINE_HS
	cfg.ThreadCount = DEFAULT_THREAD_COUNT
}

func saveConfig() {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)
	configPath := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		stdLog.Printf("Error marshaling config: %v", err)
		debugLogger.Printf("Config marshal error: %v", err)
		return
	}
	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		stdLog.Printf("Error writing config file: %v", err)
		debugLogger.Printf("Config write error: %v", err)
	}
}

func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, CONFIG_DIR_NAME)
}

func getConfigPath() string {
	return filepath.Join(getConfigDir(), CONFIG_FILE_NAME)
}

func getSysfilesDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return filepath.Join(cwd, SYSFILES_FOLDER)
}

func findChromeExecutable() (string, error) {
	// Attempt to find Chrome executable based on OS
	if isWindows() {
		paths := []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	} else if isMac() {
		p := `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
		if fileExists(p) {
			return p, nil
		}
	} else { // Assume Linux
		paths := []string{
			`/usr/bin/google-chrome`,
			`/usr/bin/chromium-browser`,
			`/usr/bin/chrome`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("Chrome executable not found")
}

func isWindows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func isMac() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func runWorker(threadIndex int, allocCtx context.Context) {
	defer executorWG.Done()
	debugLogger.Printf("Worker %d started.", threadIndex)

	userDataDir := filepath.Join(getSysfilesDir(), CHROME_USER_DATA_DIR, strconv.Itoa(threadIndex))
	os.MkdirAll(userDataDir, 0755)

	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36"

	// Create a new context with the user data directory
	taskCtx, taskCancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(debugLogger.Printf),
	)
	defer taskCancel()

	// Configure Chrome options for anti-detection
	opts := []chromedp.RunBrowserOption{
		chromedp.WindowSize(CHROME_WINDOW_WIDTH, CHROME_WINDOW_HEIGHT),
		chromedp.UserAgent(userAgent),
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.UserDataDir(userDataDir), // Persist session
	}

	// Apply options
	err := chromedp.Run(taskCtx, opts...)
	if err != nil {
		stdLog.Printf("Worker %d: Failed to run Chromedp with options: %v", threadIndex, err)
		debugLogger.Printf("Worker %d: Chromedp run error: %v", threadIndex, err)
		return
	}

	// Enable verbose logging for chromedp
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *chromelog.EventEntryAdded:
			debugLogger.Printf("Chromedp Log: %s", ev.Entry.Text)
		}
	})

	// Check if session is valid
	sessionValid := checkSession(taskCtx)
	if sessionValid {
		stdLog.Printf("Thread %d: Existing session found, no login required.", threadIndex)
		debugLogger.Printf("Thread %d: Existing session confirmed.", threadIndex)
	} else {
		stdLog.Printf("Thread %d: No valid session found, attempting to log in.", threadIndex)
		debugLogger.Printf("Thread %d: Session invalid, performing login.", threadIndex)
		if err := performLogin(taskCtx, userEmail, userPassword); err != nil {
			stdLog.Printf("Thread %d: Failed to login: %v", threadIndex, err)
			debugLogger.Printf("Thread %d: Login error: %v", threadIndex, err)
			return
		}
		stdLog.Printf("Thread %d: Logged in successfully.", threadIndex)
		debugLogger.Printf("Thread %d: Login successful.", threadIndex)
	}

	// Initial delay after login/session check (3-7 seconds)
	initialWait := time.Duration(rand.Intn(4000)+3000) * time.Millisecond
	stdLog.Printf("Thread %d: Initial wait for %v before starting to bid.", threadIndex, initialWait)
	debugLogger.Printf("Thread %d: Sleeping for %v before bidding loop.", threadIndex, initialWait)
	time.Sleep(initialWait)

	// Main bidding loop
	for atomic.LoadInt32(&stopFlag) == 0 {
		processed, err := findAndHandleSingleOrder(taskCtx, threadIndex)
		if atomic.LoadInt32(&stopFlag) != 0 {
			break
		}
		if err != nil {
			if isContextError(err) {
				stdLog.Printf("Thread %d: No orders processed or context error, continuing.", threadIndex)
				debugLogger.Printf("Thread %d: Context error: %v", threadIndex, err)
				continue
			} else {
				stdLog.Printf("Thread %d: Error processing order: %v", threadIndex, err)
				debugLogger.Printf("Thread %d: Unexpected error: %v", threadIndex, err)
				continue
			}
		}

		if !processed {
			stdLog.Printf("Thread %d: No orders processed, continuing.", threadIndex)
			debugLogger.Printf("Thread %d: No orders found during this iteration.", threadIndex)
			continue
		}

		// No sleep to ensure immediate responsiveness
	}
	debugLogger.Printf("Worker %d exiting loop.", threadIndex)
}

func checkSession(ctx context.Context) bool {
	ctxCheck, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := chromedp.Run(ctxCheck,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		debugLogger.Printf("Session check failed: %v", err)
		return false
	}
	return true
}

func performLogin(ctx context.Context, email, password string) error {
	ctxLogin, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctxLogin,
		chromedp.Navigate("https://essayshark.com/log-in.html"),
		chromedp.WaitVisible(`input[name="login"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="password"]`, chromedp.ByQuery),
		chromedp.Clear(`input[name="login"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="login"]`, email),
		chromedp.Clear(`input[name="password"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="password"]`, password),
		chromedp.Click(`button.bb-button[type="submit"]`, chromedp.NodeVisible),
		chromedp.WaitVisible(`#available_orders_list_container`, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error during login: %w", err)
	}

	return nil
}

func findAndHandleSingleOrder(ctx context.Context, threadIndex int) (bool, error) {
	ctxOrders, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err := chromedp.Run(ctxOrders,
		chromedp.Navigate("https://essayshark.com/writer/orders/"),
		chromedp.WaitVisible(`tr.order_container`, chromedp.ByQuery),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error navigating to orders page: %v", threadIndex, err)
		return false, err
	}

	var result struct {
		Links     []string `json:"links"`
		Services  []string `json:"services"`
		Deadlines []string `json:"deadlines"`
	}

	err = chromedp.Run(ctxOrders,
		chromedp.Evaluate(`
			(function(){
				let rows = document.querySelectorAll("tr.order_container");
				let data = {links: [], services: [], deadlines: []};
				for (let row of rows) {
					let topicLink = row.querySelector("td.topictitle a");
					let serviceEl = row.querySelector("div.service_type");
					let deadlineEl = row.querySelector("td.td_deadline span.d-deadline + span.d-left");
					data.links.push(topicLink ? topicLink.href : "");
					data.services.push(serviceEl ? serviceEl.textContent.trim() : "");
					data.deadlines.push(deadlineEl ? deadlineEl.textContent.trim() : "");
				}
				return data;
			})()
		`, &result),
	)
	if err != nil {
		debugLogger.Printf("Thread %d: Error evaluating orders: %v", threadIndex, err)
		return false, err
	}

	orderLinks := result.Links
	serviceTypes := result.Services
	deadlineTexts := result.Deadlines

	for i, orderUrl := range orderLinks {
		if orderUrl == "" {
			continue
		}
		if atomic.LoadInt32(&stopFlag) != 0 {
			return false, nil
		}

		orderLock.Lock()
		_, alreadyProcessing := orderToThreadMap[orderUrl]
		if alreadyProcessing {
			orderLock.Unlock()
			continue
		}
		orderToThreadMap[orderUrl] = threadIndex
		orderLock.Unlock()

		if shouldDiscardServiceType(serviceTypes[i]) {
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		dh := convertDeadlineToHours(deadlineTexts[i])
		if dh != -1 && (dh < cfg.MinDeadlineHours || dh > cfg.MaxDeadlineHours) {
			stdLog.Printf("Thread %d: Order %s deadline (%dh) out of range.", threadIndex, orderUrl, dh)
			discardOrder(orderUrl)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Open order details
		ctxOrderDetail, cancelOrderDetail := context.WithTimeout(ctx, 20*time.Second)
		defer cancelOrderDetail()

		err = chromedp.Run(ctxOrderDetail,
			chromedp.Navigate(orderUrl),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
		)
		if err != nil {
			stdLog.Printf("Thread %d: Failed to open order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Handle the order (place bid or apply)
		err = handleOrder(ctxOrderDetail, orderUrl, threadIndex)
		if err != nil {
			stdLog.Printf("Thread %d: Error handling order %s: %v", threadIndex, orderUrl, err)
			orderLock.Lock()
			delete(orderToThreadMap, orderUrl)
			orderLock.Unlock()
			continue
		}

		// Remove from processing map
		orderLock.Lock()
		delete(orderToThreadMap, orderUrl)
		orderLock.Unlock()

		return true, nil // Processed an order
	}

	return false, nil // No orders processed
}

func handleOrder(ctx context.Context, orderUrl string, threadIndex int) error {
	isFixed, err := isFixedPriceOrder(ctx)
	if err != nil {
		return fmt.Errorf("error checking if order is fixed-price: %w", err)
	}

	if hasCountdown, seconds := checkCountdown(ctx); hasCountdown {
		stdLog.Printf("Thread %d: Order %s has countdown: %d seconds. Waiting...", threadIndex, orderUrl, seconds)
		debugLogger.Printf("Thread %d: Waiting for %d seconds due to countdown.", threadIndex, seconds)
		time.Sleep(time.Duration(seconds) * time.Second)
	}

	if hasAttachments(ctx) {
		err := downloadFileIfAvailable(ctx)
		if err != nil {
			stdLog.Printf("Thread %d: Error downloading attachments for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Attachment download error: %v", threadIndex, err)
		}
	}

	if isFixed {
		stdLog.Printf("Thread %d: Order %s is fixed-price. Applying directly.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Applying for fixed-price order.", threadIndex)
		err = applyForOrder(ctx)
		if err != nil {
			return fmt.Errorf("error applying for fixed-price order: %w", err)
		}
	} else {
		stdLog.Printf("Thread %d: Order %s is not fixed-price. Placing bid.", threadIndex, orderUrl)
		debugLogger.Printf("Thread %d: Placing bid on order.", threadIndex)
		err = placeBid(ctx, threadIndex)
		if err != nil {
			return fmt.Errorf("error placing bid: %w", err)
		}
	}

	if cfg.MessageEnabled {
		err = sendMessageToClient(ctx, cfg.MessageText)
		if err != nil {
			stdLog.Printf("Thread %d: Error sending message for order %s: %v", threadIndex, orderUrl, err)
			debugLogger.Printf("Thread %d: Message sending error: %v", threadIndex, err)
		}
	}

	// Navigate back to orders page without delay
	ctxBack, cancelBack := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBack()
	err = chromedp.Run(ctxBack, chromedp.Navigate("https://essayshark.com/writer/orders/"))
	if err != nil {
		stdLog.Printf("Thread %d: Error navigating back to orders page: %v", threadIndex, err)
		debugLogger.Printf("Thread %d: Navigation back error: %v", threadIndex, err)
	}

	return nil
}

func isFixedPriceOrder(ctx context.Context) (bool, error) {
	var bodyText string
	ctxCheck, cancelCheck := context.WithTimeout(ctx, 10*time.Second)
	defer cancelCheck()

	err := chromedp.Run(ctxCheck, chromedp.Text("body", &bodyText))
	if err != nil {
		return false, fmt.Errorf("error retrieving page body: %w", err)
	}

	if strings.Contains(strings.ToLower(bodyText), "this field is disabled for fixed-price orders") {
		return true, nil
	}
	return false, nil
}

func checkCountdown(ctx context.Context) (bool, int) {
	var countdownText string
	ctxCount, cancelCount := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCount()

	err := chromedp.Run(ctxCount,
		chromedp.Text(`#id_read_timeout_sec`, &countdownText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil || countdownText == "" {
		return false, 0
	}
	var sec int
	fmt.Sscanf(countdownText, "%d", &sec)
	return true, sec
}

func hasAttachments(ctx context.Context) bool {
	var bodyText string
	ctxAttach, cancelAttach := context.WithTimeout(ctx, 5*time.Second)
	defer cancelAttach()

	err := chromedp.Run(ctxAttach, chromedp.Text("body", &bodyText))
	if err != nil {
		debugLogger.Printf("Error checking attachments: %v", err)
		return false
	}
	return strings.Contains(strings.ToLower(bodyText), "uploaded additional materials:")
}

func downloadFileIfAvailable(ctx context.Context) error {
	// Implement actual download logic if needed
	// For now, just log the action
	stdLog.Println("Simulated file download to downloads directory.")
	debugLogger.Println("Simulated file download action.")
	return nil
}

func applyForOrder(ctx context.Context) error {
	ctxApply, cancelApply := context.WithTimeout(ctx, 5*time.Second)
	defer cancelApply()

	err := chromedp.Run(ctxApply,
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error clicking apply button: %w", err)
	}
	return nil
}

func placeBid(ctx context.Context, threadIndex int) error {
	ctxBid, cancelBid := context.WithTimeout(ctx, 10*time.Second)
	defer cancelBid()

	err := chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", "-1.00", chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting bid value or clicking apply: %w", err)
	}

	var errText string
	err = chromedp.Run(ctxBid,
		chromedp.Text("#id_bid4-error", &errText, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error retrieving bid error message: %w", err)
	}

	minBid := extractMinimumBid(errText)
	if minBid <= 0 {
		stdLog.Printf("Thread %d: Invalid minimum bid extracted, skipping.", threadIndex)
		debugLogger.Printf("Thread %d: Extracted minimum bid is invalid: %f", threadIndex, minBid)
		return nil
	}

	err = chromedp.Run(ctxBid,
		chromedp.SetValue("#id_bid4", fmt.Sprintf("%.2f", minBid), chromedp.ByID),
		chromedp.Click("#apply_order", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error setting minimum bid or clicking apply: %w", err)
	}

	return nil
}

func extractMinimumBid(errorMessage string) float64 {
	var amount float64
	fmt.Sscanf(errorMessage, "Minimum bid is $%f", &amount)
	return amount
}

func sendMessageToClient(ctx context.Context, msg string) error {
	ctxMsg, cancelMsg := context.WithTimeout(ctx, 5*time.Second)
	defer cancelMsg()

	err := chromedp.Run(ctxMsg,
		chromedp.SetValue("#id_body", msg, chromedp.ByID),
		chromedp.Click("#id_send_message", chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}

	return nil
}

func discardOrder(orderUrl string) {
	stdLog.Printf("Discarding order %s.", orderUrl)
	debugLogger.Printf("Order %s discarded based on service type.", orderUrl)
}

func shouldDiscardServiceType(serviceType string) bool {
	serviceTypeLower := strings.ToLower(serviceType)
	if cfg.DiscardAssignments && serviceTypeLower == "writing help or assignments" {
		return true
	}
	if cfg.DiscardEditing && serviceTypeLower == "editing" {
		return true
	}
	return false
}

func convertDeadlineToHours(deadlineText string) int {
	if deadlineText == "" {
		return -1
	}
	var days, hours int
	_, err := fmt.Sscanf(deadlineText, "%dd %dh", &days, &hours)
	if err != nil {
		return -1
	}
	return days*24 + hours
}

func loadConfig() {
	configPath := getConfigPath()
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		stdLog.Println("Config file not found. Using default settings.")
		debugLogger.Println("Config load error:", err)
		setDefaultConfig()
		return
	}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		stdLog.Println("Error parsing config file. Using default settings.")
		debugLogger.Println("Config parse error:", err)
		setDefaultConfig()
	}
}

func setDefaultConfig() {
	cfg.MessageEnabled = false
	cfg.MessageText = ""
	cfg.DiscardAssignments = false
	cfg.DiscardEditing = false
	cfg.MinDeadlineHours = DEFAULT_MIN_DEADLINE_HS
	cfg.MaxDeadlineHours = DEFAULT_MAX_DEADLINE_HS
	cfg.ThreadCount = DEFAULT_THREAD_COUNT
}

func saveConfig() {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)
	configPath := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		stdLog.Printf("Error marshaling config: %v", err)
		debugLogger.Printf("Config marshal error: %v", err)
		return
	}
	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		stdLog.Printf("Error writing config file: %v", err)
		debugLogger.Printf("Config write error: %v", err)
	}
}

func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, CONFIG_DIR_NAME)
}

func getConfigPath() string {
	return filepath.Join(getConfigDir(), CONFIG_FILE_NAME)
}

func getSysfilesDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return filepath.Join(cwd, SYSFILES_FOLDER)
}

func findChromeExecutable() (string, error) {
	// Attempt to find Chrome executable based on OS
	if isWindows() {
		paths := []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	} else if isMac() {
		p := `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
		if fileExists(p) {
			return p, nil
		}
	} else { // Assume Linux
		paths := []string{
			`/usr/bin/google-chrome`,
			`/usr/bin/chromium-browser`,
			`/usr/bin/chrome`,
		}
		for _, p := range paths {
			if fileExists(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("Chrome executable not found")
}

func isWindows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func isMac() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
