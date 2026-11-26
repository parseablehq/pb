package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var k8sDeploymentCmd = &cobra.Command{
	Use:     "deployment <name>",
	Short:   "Open a deployment metrics dashboard from Parseable telemetry",
	Example: "  pb k8s deployment payment -n shop --context production -i",
	Args:    cobra.ExactArgs(1),
	RunE:    runK8sDeployment,
}

var k8sStatefulSetCmd = &cobra.Command{
	Use:     "statefulset <name>",
	Aliases: []string{"sts"},
	Short:   "Open a StatefulSet metrics dashboard from Parseable telemetry",
	Example: "  pb k8s statefulset database -n shop --context production -i",
	Args:    cobra.ExactArgs(1),
	RunE:    runK8sDeployment,
}

func init() {
	K8sCmd.AddCommand(k8sDeploymentCmd)
	K8sCmd.AddCommand(k8sStatefulSetCmd)
	for _, command := range []*cobra.Command{k8sDeploymentCmd, k8sStatefulSetCmd} {
		command.Flags().String("context", "", "Kubeconfig context configured with pb k8s setup")
		command.Flags().StringP("namespace", "n", "", "Workload namespace")
		command.Flags().String("from", "1h", "Look back this far (15m, 1h, 6h or 24h)")
		command.Flags().BoolP("interactive", "i", false, "Open the metrics dashboard")
	}
}

type k8sDeploymentRequest struct {
	Profile    config.Profile
	Mapping    config.K8sContext
	Context    string
	Namespace  string
	Deployment string
	Kind       string
	From       string
}

// runK8sDeployment opens the same dashboard for a Deployment or StatefulSet.
func runK8sDeployment(cmd *cobra.Command, args []string) error {
	contextName, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	from, _ := cmd.Flags().GetString("from")
	interactive, _ := cmd.Flags().GetBool("interactive")
	if contextName == "" || namespace == "" {
		return newCLIError(ErrorInvalidInput, "--context and --namespace are required", nil)
	}
	if _, err := parseTimeStr(from); err != nil {
		return newCLIError(ErrorInvalidInput, "invalid --from value", err)
	}
	if !interactive || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return newCLIError(ErrorInvalidInput, "workload dashboard requires -i and a terminal", nil)
	}
	conf, err := config.ReadConfigFromFile()
	if err != nil {
		return err
	}
	mapping, ok := conf.K8sContexts[contextName]
	if !ok {
		return newCLIError(ErrorNotFound, fmt.Sprintf("context %q is not configured; run pb k8s setup", contextName), nil)
	}
	profile, ok := conf.Profiles[mapping.Profile]
	if !ok {
		return newCLIError(ErrorNotFound, fmt.Sprintf("pb profile %q no longer exists", mapping.Profile), nil)
	}
	request := k8sDeploymentRequest{Profile: profile, Mapping: mapping, Context: contextName, Namespace: namespace, Deployment: args[0], Kind: cmd.Name(), From: from}
	_, err = tea.NewProgram(newK8sDeploymentModel(request), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// k8sDeploymentSelector matches pods belonging to the named workload.
func k8sDeploymentSelector(request k8sDeploymentRequest, name string) string {
	selector := name + `{k8s.namespace.name="` + escapeK8sPromQLLabel(request.Namespace) + `",k8s.pod.name=~"` + escapeK8sPromQLLabel(regexp.QuoteMeta(request.Deployment)+`-.+`) + `"`
	if request.Mapping.ClusterName != "" {
		selector += `,k8s.cluster.name="` + escapeK8sPromQLLabel(request.Mapping.ClusterName) + `"`
	}
	return selector + "}"
}

func fetchK8sDeploymentSignal(request k8sDeploymentRequest, name string) (map[string]k8sPodMetric, error) {
	return fetchK8sDeploymentExpression(request, "sum by (k8s.pod.name) ("+k8sDeploymentSelector(request, name)+")")
}

// fetchK8sDeploymentExpression keeps a separate time series for each pod.
func fetchK8sDeploymentExpression(request k8sDeploymentRequest, expression string) (map[string]k8sPodMetric, error) {
	start, end, err := k8sMetricWindow(request.From)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("query", expression)
	params.Set("stream", request.Mapping.MetricsDataset)
	params.Set("start", start.UTC().Format(time.RFC3339))
	params.Set("end", end.Format(time.RFC3339))
	params.Set("step", strconv.Itoa(k8sMetricStep(start, end)))
	endpoint, err := url.JoinPath(request.Profile.URL, "prometheus/api/v1/query_range")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := internalHTTP.AddAuthHeaders(req, &request.Profile); err != nil {
		return nil, err
	}
	client := internalHTTP.DefaultClient(&request.Profile)
	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := readLimitedErrorPreview(resp.Body)
		return nil, fmt.Errorf("workload metrics: %s: %s", resp.Status, preview)
	}
	var result promqlResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status == "error" {
		return nil, fmt.Errorf("workload metrics: %s", result.Error)
	}
	out := make(map[string]k8sPodMetric)
	for _, series := range result.Data.Result {
		pod := series.Metric["k8s.pod.name"]
		if pod == "" || !strings.HasPrefix(pod, request.Deployment+"-") {
			continue
		}
		var metric k8sPodMetric
		metric.windowEnd = end
		for _, point := range series.Values {
			if len(point) != 2 {
				continue
			}
			stamp, ok := point[0].(float64)
			if !ok {
				continue
			}
			value, err := strconv.ParseFloat(fmt.Sprint(point[1]), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			at := time.Unix(int64(stamp), 0)
			metric.values = append(metric.values, value)
			metric.times = append(metric.times, at)
			if at.After(metric.at) {
				metric.latest, metric.at = value, at
			}
		}
		if !metric.at.IsZero() {
			out[pod] = metric
		}
	}
	return out, nil
}

type k8sDeploymentData struct {
	cpu, memory                map[string]k8sPodMetric
	cpuRequest, cpuLimit       map[string]k8sPodMetric
	memoryRequest, memoryLimit map[string]k8sPodMetric
	requests, errors           map[string]k8sPodMetric
	duration                   map[string]k8sPodMetric
	err                        error
	updatedAt                  time.Time
}

// loadK8sDeployment fetches resource and HTTP metrics for the view.
func loadK8sDeployment(request k8sDeploymentRequest) k8sDeploymentData {
	var data k8sDeploymentData
	var errorsBySignal [6]error
	tasks := []struct {
		name string
		into *map[string]k8sPodMetric
	}{
		{"k8s.pod.cpu.usage", &data.cpu},
		{"k8s.pod.memory.usage", &data.memory},
		{"k8s.container.cpu_request", &data.cpuRequest},
		{"k8s.container.cpu_limit", &data.cpuLimit},
		{"k8s.container.memory_request", &data.memoryRequest},
		{"k8s.container.memory_limit", &data.memoryLimit},
	}
	var group sync.WaitGroup
	for i, task := range tasks {
		group.Add(1)
		go func() {
			defer group.Done()
			*task.into, errorsBySignal[i] = fetchK8sDeploymentSignal(request, task.name)
		}()
	}
	group.Wait()
	for _, err := range errorsBySignal {
		if err != nil {
			data.err = err
			break
		}
	}
	pods := k8sDeploymentPods(data)
	if len(pods) > 0 {
		data.requests, data.errors, data.duration = fetchK8sDeploymentHTTP(request, pods[0])
	}
	data.updatedAt = time.Now()
	return data
}

func k8sDeploymentPods(data k8sDeploymentData) []string {
	set := make(map[string]bool)
	for pod := range data.cpu {
		set[pod] = true
	}
	for pod := range data.memory {
		set[pod] = true
	}
	pods := make([]string, 0, len(set))
	for pod := range set {
		pods = append(pods, pod)
	}
	sort.Strings(pods)
	return pods
}

type k8sDeploymentLoadedMsg k8sDeploymentData

// k8sDeploymentModel holds dashboard data, controls, and scroll position.
type k8sDeploymentModel struct {
	request   k8sDeploymentRequest
	data      k8sDeploymentData
	loading   bool
	width     int
	height    int
	scroll    int
	aggregate string
	perPod    bool
}

func newK8sDeploymentModel(request k8sDeploymentRequest) k8sDeploymentModel {
	return k8sDeploymentModel{request: request, loading: true, width: 100, height: 25, aggregate: "total", perPod: true}
}

func (m k8sDeploymentModel) Init() tea.Cmd {
	return tea.Batch(m.load(), k8sRefreshTick())
}

func (m k8sDeploymentModel) load() tea.Cmd {
	request := m.request
	return func() tea.Msg { return k8sDeploymentLoadedMsg(loadK8sDeployment(request)) }
}

func (m k8sDeploymentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(30, msg.Width), max(8, msg.Height)
	case k8sDeploymentLoadedMsg:
		m.data, m.loading = k8sDeploymentData(msg), false
	case k8sRefreshMsg:
		if m.loading {
			return m, k8sRefreshTick()
		}
		m.loading = true
		return m, tea.Batch(m.load(), k8sRefreshTick())
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "1", "2", "3", "4":
			m.request.From = map[string]string{"1": "15m", "2": "1h", "3": "6h", "4": "24h"}[msg.String()]
			m.scroll, m.loading = 0, true
			return m, m.load()
		case "t":
			m.aggregate = "total"
			m.perPod = false
		case "a":
			m.aggregate = "avg"
			m.perPod = false
		case "n":
			m.aggregate = "min"
			m.perPod = false
		case "x":
			m.aggregate = "max"
			m.perPod = false
		case "p":
			m.perPod = !m.perPod
		case "r":
			m.loading = true
			return m, m.load()
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-7))
		case "pgdown":
			m.scroll += max(1, m.height-7)
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.scroll = max(0, m.scroll-3)
			case tea.MouseButtonWheelDown:
				m.scroll += 3
			}
		}
	}
	return m, nil
}

var _ tea.Model = k8sDeploymentModel{}

// View renders the workload dashboard with a fixed header and footer.
func (m k8sDeploymentModel) View() string {
	p := ui.Active
	width := max(30, m.width)
	muted := lipgloss.NewStyle().Foreground(p.Mute)
	pods := k8sDeploymentPods(m.data)
	kind := m.request.Kind
	if kind == "" {
		kind = "deployment"
	}
	updated := "loading"
	if !m.data.updatedAt.IsZero() {
		updated = m.data.updatedAt.UTC().Format("15:04:05 UTC")
	}
	header := []string{
		lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("PB  /  KUBERNETES  /  " + strings.ToUpper(kind)),
	}
	header = append(header, deploymentMetadata(m, kind, len(pods), width)...)
	header = append(header, lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(muted.Render("Updated "+updated+"   ·   auto refresh 15s")))
	header = append(header, strings.Split(deploymentControls(m, width), "\n")...)
	var body []string
	if m.loading && m.data.updatedAt.IsZero() {
		body = append(body, muted.Render("Loading "+kind+" metrics..."))
	} else if m.data.err != nil && len(pods) == 0 {
		body = append(body, lipgloss.NewStyle().Foreground(p.Err).Render(truncateK8sLine(m.data.err.Error(), width)))
	} else if len(pods) == 0 {
		body = append(body, muted.Render("No pod metrics in this time range."))
	} else {
		cpu := aggregateK8sMetric(m.data.cpu, pods, m.aggregate)
		memory := aggregateK8sMetric(m.data.memory, pods, m.aggregate)
		cpuWidth, memoryWidth := width, width
		if width >= 88 && !cpu.at.IsZero() && !memory.at.IsZero() {
			cpuWidth = (width - 2) / 2
			memoryWidth = width - cpuWidth - 2
		}
		var summaries []string
		if !cpu.at.IsZero() {
			summaries = append(summaries, deploymentSummary("CPU", cpu, aggregateK8sMetric(m.data.cpuRequest, pods, m.aggregate), aggregateK8sMetric(m.data.cpuLimit, pods, m.aggregate), true, cpuWidth))
		}
		if !memory.at.IsZero() {
			summaries = append(summaries, deploymentSummary("Memory", memory, aggregateK8sMetric(m.data.memoryRequest, pods, m.aggregate), aggregateK8sMetric(m.data.memoryLimit, pods, m.aggregate), false, memoryWidth))
		}
		body = appendDeploymentPanels(body, summaries, width)
		var charts []string
		if !cpu.at.IsZero() {
			charts = append(charts, deploymentChart("CPU", m.data.cpu, pods, m.aggregate, m.perPod, m.request.From, m.data.updatedAt, cpuWidth, true))
		}
		if !memory.at.IsZero() {
			charts = append(charts, deploymentChart("Memory", m.data.memory, pods, m.aggregate, m.perPod, m.request.From, m.data.updatedAt, memoryWidth, false))
		}
		body = appendDeploymentPanels(body, charts, width)
		body = append(body, deploymentRequestSection(m, pods, width)...)
		if m.data.err != nil {
			body = append(body, lipgloss.NewStyle().Foreground(p.Err).Render(truncateK8sLine(m.data.err.Error(), width)))
		}
	}
	footer := muted.Render(truncateK8sLine("1–4 range   t/a/n/x aggregate   p per-pod   ↑↓ scroll   r refresh   q back", width))
	height := max(8, m.height)
	if len(header) > height-2 {
		header = header[:height-2]
	}
	visible := max(0, height-len(header)-2)
	scroll := min(m.scroll, max(0, len(body)-visible))
	end := min(len(body), scroll+visible)
	lines := append(append([]string{}, header...), body[scroll:end]...)
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(append(lines, "", footer), "\n"))
}

func deploymentMetadata(m k8sDeploymentModel, kind string, podCount, width int) []string {
	p := ui.Active
	label := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	value := lipgloss.NewStyle().Foreground(p.Text)
	field := func(name, content string) string {
		return label.Render(name+" ") + value.Render(content)
	}
	left := []string{field("namespace", m.request.Namespace), field(kind, m.request.Deployment), field("pods", fmt.Sprint(podCount))}
	right := []string{field("profile", m.request.Mapping.Profile), field("context", m.request.Context)}
	leftWidth := max(lipgloss.Width(left[0]), lipgloss.Width(left[1]), lipgloss.Width(left[2]))
	rightWidth := max(lipgloss.Width(right[0]), lipgloss.Width(right[1]))
	if leftWidth+rightWidth+3 <= width {
		return []string{
			left[0] + strings.Repeat(" ", width-lipgloss.Width(left[0])-lipgloss.Width(right[0])) + right[0],
			left[1] + strings.Repeat(" ", width-lipgloss.Width(left[1])-lipgloss.Width(right[1])) + right[1],
			left[2],
		}
	}
	stacked := append(left, right...)
	for i, line := range stacked {
		stacked[i] = lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return stacked
}

func deploymentControls(m k8sDeploymentModel, width int) string {
	p := ui.Active
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Padding(0, 1)
	var rangeParts []string
	for _, span := range []string{"15m", "1h", "6h", "24h"} {
		rangeParts = append(rangeParts, deploymentButton(span, m.request.From == span))
	}
	left := border.Render(strings.Join(rangeParts, "  "))
	var aggregateParts []string
	for _, item := range []struct{ label, key string }{{"total", "t"}, {"avg", "a"}, {"min", "n"}, {"max", "x"}} {
		aggregateParts = append(aggregateParts, deploymentButton(item.label, m.aggregate == item.label))
	}
	aggregateParts = append(aggregateParts, deploymentButton("per pod", m.perPod))
	aggregate := border.Render(strings.Join(aggregateParts, "  "))
	right := aggregate
	if width >= lipgloss.Width(left)+lipgloss.Width(right)+4 {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right)), right)
	}
	if width < lipgloss.Width(right) {
		return lipgloss.NewStyle().Foreground(p.Mute).Render("1–4 range   t/a/n/x aggregate   p per pod")
	}
	return left + "\n" + lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(right)
}

func deploymentButton(label string, selected bool) string {
	p := ui.Active
	if selected {
		return lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("[" + label + "]")
	}
	return lipgloss.NewStyle().Foreground(p.Mute).Render(" " + label + " ")
}

func appendDeploymentPanels(body, panels []string, width int) []string {
	if width >= 88 && len(panels) >= 2 {
		return append(body, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, panels[0], "  ", panels[1]), "\n")...)
	}
	for _, panel := range panels {
		body = append(body, strings.Split(panel, "\n")...)
	}
	return body
}

// aggregateK8sMetric combines pod samples at each timestamp for the selected mode.
func aggregateK8sMetric(signals map[string]k8sPodMetric, pods []string, mode string) k8sPodMetric {
	byTime := make(map[int64][]float64)
	var result k8sPodMetric
	for _, pod := range pods {
		metric := signals[pod]
		if metric.windowEnd.After(result.windowEnd) {
			result.windowEnd = metric.windowEnd
		}
		for i, at := range metric.times {
			if i < len(metric.values) {
				byTime[at.Unix()] = append(byTime[at.Unix()], metric.values[i])
			}
		}
	}
	stamps := make([]int64, 0, len(byTime))
	for stamp := range byTime {
		stamps = append(stamps, stamp)
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i] < stamps[j] })
	for _, stamp := range stamps {
		values := byTime[stamp]
		value := values[0]
		switch mode {
		case "min":
			for _, next := range values[1:] {
				value = min(value, next)
			}
		case "max":
			for _, next := range values[1:] {
				value = max(value, next)
			}
		default:
			for _, next := range values[1:] {
				value += next
			}
			if mode == "avg" {
				value /= float64(len(values))
			}
		}
		result.values = append(result.values, value)
		result.times = append(result.times, time.Unix(stamp, 0))
		result.latest, result.at = value, time.Unix(stamp, 0)
	}
	return result
}

func deploymentSummary(name string, metric, request, limit k8sPodMetric, cpu bool, width int) string {
	p := ui.Active
	accent := p.Accent
	number := fmt.Sprintf("%.1f", metric.latest/(1<<20))
	readingUnit := "MiB"
	detail := "memory working set"
	if cpu {
		accent = p.Active
		detail = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", metric.latest), "0"), ".") + " cores"
		number = fmt.Sprintf("%.3f", metric.latest)
		readingUnit = "cores"
		if metric.latest < 0.1 {
			number = fmt.Sprintf("%.2f", metric.latest*1000)
			readingUnit = "mCPU"
		}
	}
	label := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(strings.ToUpper(name) + " USAGE")
	muted := lipgloss.NewStyle().Foreground(p.Mute)
	innerWidth := max(1, width-4)
	leftWidth := max(16, innerWidth/2)
	readingStyle := lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	reading := lipgloss.JoinHorizontal(lipgloss.Bottom, readingStyle.Render(k8sLargeNumber(number)), " "+readingStyle.Render(readingUnit))
	readingWidth := innerWidth
	if innerWidth >= 56 {
		readingWidth = leftWidth
	}
	if lipgloss.Width(reading) > readingWidth {
		reading = readingStyle.Render(number + " " + readingUnit)
	}
	left := []string{reading, muted.Render(detail)}
	if len(metric.values) > 1 {
		left = append(left, "", lipgloss.NewStyle().Foreground(accent).Render(k8sSparkline(metric.values, min(40, max(1, readingWidth-2)))))
	}
	content := strings.Join(left, "\n")
	amount := func(value float64) string {
		if cpu {
			millis := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", value*1000), "0"), ".")
			return millis + " mCPU"
		}
		return fmt.Sprintf("%.1f MiB", value/(1<<20))
	}
	requestValue, limitValue := "not set", "not set"
	if request.latest > 0 {
		requestValue = amount(request.latest)
	}
	if limit.latest > 0 {
		limitValue = amount(limit.latest)
	}
	right := []string{
		readingStyle.Render("Request: " + requestValue),
		readingStyle.Render("Limit:   " + limitValue),
	}
	if limit.latest > 0 {
		percent := metric.latest / limit.latest * 100
		percentText := fmt.Sprintf("%.1f%%", percent)
		if percent > 0 && percent < 1 {
			percentText = fmt.Sprintf("%.2f%%", percent)
		}
		right = append(right, "", lipgloss.NewStyle().Foreground(accent).Bold(true).Render(percentText), muted.Render("of limit used"))
	}
	rightWidth := max(1, innerWidth-leftWidth-4)
	if innerWidth < 56 {
		rightWidth = max(1, innerWidth-2)
	}
	if limit.latest > 0 {
		right = append(right, k8sUsageBar(metric.latest, limit.latest, rightWidth, accent))
	}
	if innerWidth >= 56 {
		rows := strings.Split(content, "\n")
		for index, row := range right {
			if index >= len(rows) {
				rows = append(rows, "")
			}
			if row != "" {
				rows[index] += strings.Repeat(" ", max(2, leftWidth-lipgloss.Width(rows[index])+2)) + row
			}
		}
		content = strings.Join(rows, "\n")
	} else {
		content += "\n\n" + strings.Join(right, "\n")
	}
	return lipgloss.NewStyle().Width(width-2).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Render(label + "\n" + content)
}

func k8sLargeNumber(number string) string {
	glyphs := map[rune][5]string{
		'0': {"███", "█ █", "█ █", "█ █", "███"},
		'1': {"  █", "  █", "  █", "  █", "  █"},
		'2': {"███", "  █", "███", "█  ", "███"},
		'3': {"███", "  █", "███", "  █", "███"},
		'4': {"█ █", "█ █", "███", "  █", "  █"},
		'5': {"███", "█  ", "███", "  █", "███"},
		'6': {"███", "█  ", "███", "█ █", "███"},
		'7': {"███", "  █", "  █", "  █", "  █"},
		'8': {"███", "█ █", "███", "█ █", "███"},
		'9': {"███", "█ █", "███", "  █", "███"},
		'.': {" ", " ", " ", " ", "█"},
	}
	lines := make([]string, 5)
	for _, digit := range number {
		glyph, ok := glyphs[digit]
		if !ok {
			continue
		}
		for row := range lines {
			if lines[row] != "" {
				lines[row] += " "
			}
			lines[row] += glyph[row]
		}
	}
	return strings.Join(lines, "\n")
}

func k8sUsageBar(usage, limit float64, width int, color lipgloss.Color) string {
	eighths := int(math.Round(usage / limit * float64(width*8)))
	if usage > 0 && eighths == 0 {
		eighths = 1
	}
	eighths = min(width*8, max(0, eighths))
	full, partial := eighths/8, eighths%8
	fill := strings.Repeat("█", full)
	if partial > 0 {
		fill += []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}[partial]
	}
	remaining := width - full
	if partial > 0 {
		remaining--
	}
	return lipgloss.NewStyle().Foreground(color).Render(fill) +
		lipgloss.NewStyle().Foreground(ui.Active.Mute).Render(strings.Repeat("░", remaining))
}

func deploymentChart(name string, signals map[string]k8sPodMetric, pods []string, mode string, perPod bool, span string, end time.Time, width int, cpu bool) string {
	if cpu {
		return k8sResourceChart(name, signals, pods, mode, perPod, span, end, width, "cores", 1, true)
	}
	return k8sResourceChart(name, signals, pods, mode, perPod, span, end, width, "MiB", 1<<20, true)
}

func podChart(name string, metric k8sPodMetric, pod, span string, end time.Time, width int, cpu bool) string {
	if cpu {
		return k8sResourceChart(name, map[string]k8sPodMetric{pod: metric}, []string{pod}, "total", true, span, end, width, "cores", 1, false)
	}
	return k8sResourceChart(name, map[string]k8sPodMetric{pod: metric}, []string{pod}, "total", true, span, end, width, "MiB", 1<<20, false)
}

// k8sResourceChart draws real samples, either per pod or as one aggregate.
func k8sResourceChart(name string, signals map[string]k8sPodMetric, pods []string, mode string, perPod bool, span string, end time.Time, width int, unit string, scale float64, byPod bool) string {
	p := ui.Active
	if end.IsZero() {
		end = time.Now()
	}
	start := end
	if duration, err := time.ParseDuration(span); err == nil {
		start = end.Add(-duration)
	} else if parsed, err := parseTimeStr(span); err == nil {
		start = parsed
	}
	innerWidth := max(24, width-4)
	chartHeight := 15
	type resourceSeries struct {
		name   string
		metric k8sPodMetric
		color  lipgloss.Color
	}
	colors := []lipgloss.Color{p.Accent, p.Active, p.Number, p.String, p.Warn}
	var series []resourceSeries
	if perPod {
		for i, pod := range pods {
			if metric := signals[pod]; !metric.at.IsZero() {
				color := colors[i%len(colors)]
				if pod == "5xx errors" {
					color = p.Err
				}
				series = append(series, resourceSeries{pod, metric, color})
			}
		}
	} else if metric := aggregateK8sMetric(signals, pods, mode); !metric.at.IsZero() {
		series = append(series, resourceSeries{mode, metric, p.Accent})
	}
	var metricEnd time.Time
	for _, signal := range series {
		if signal.metric.windowEnd.After(metricEnd) {
			metricEnd = signal.metric.windowEnd
		}
	}
	if !metricEnd.IsZero() {
		end = metricEnd
	}
	if duration, err := time.ParseDuration(span); err == nil {
		start = end.Add(-duration)
	}
	title := strings.ToUpper(name) + "  /  " + unit
	if byPod {
		title += "  /  BY POD"
	}
	header := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(title)
	rangeLabel := lipgloss.NewStyle().Foreground(p.Mute).Render(start.UTC().Format("15:04") + "  →  " + end.UTC().Format("15:04") + "  (" + span + ") UTC")
	if lipgloss.Width(header)+2+lipgloss.Width(rangeLabel) <= innerWidth {
		header += strings.Repeat(" ", innerWidth-lipgloss.Width(header)-lipgloss.Width(rangeLabel)) + rangeLabel
	} else {
		header += "\n" + rangeLabel
	}
	if len(series) == 0 {
		return lipgloss.NewStyle().Width(width-2).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Render(header + "\n\nNo samples in this time range.")
	}
	low, high := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for _, value := range s.metric.values {
			low, high = min(low, value/scale), max(high, value/scale)
		}
	}
	padding := max((high-low)*0.12, high*0.04)
	if unit == "cores" {
		low = 0
	} else {
		low = max(0, low-padding)
	}
	high += padding
	if high <= low {
		high = low + 1
	}
	axis := lipgloss.NewStyle().Foreground(p.Faint)
	chart := timeserieslinechart.New(
		innerWidth, chartHeight,
		timeserieslinechart.WithAxesStyles(axis, axis),
		timeserieslinechart.WithTimeRange(start, end),
		timeserieslinechart.WithYRange(low, high),
		timeserieslinechart.WithXYSteps(max(12, innerWidth/3), 4),
		timeserieslinechart.WithXLabelFormatter(func(_ int, value float64) string {
			return time.Unix(int64(value), 0).UTC().Format("15:04")
		}),
		timeserieslinechart.WithYLabelFormatter(func(_ int, value float64) string {
			switch {
			case unit == "cores":
				return fmt.Sprintf("%.4f", value)
			case value < 10:
				return fmt.Sprintf("%.1f", value)
			default:
				return fmt.Sprintf("%.0f", value)
			}
		}),
	)
	var legend []string
	for _, s := range series {
		for run, segment := range k8sChartSegments(s.metric, start, end, scale) {
			dataset := fmt.Sprintf("%s/%d", s.name, run)
			chart.SetDataSetStyle(dataset, lipgloss.NewStyle().Foreground(s.color))
			for _, point := range segment {
				chart.PushDataSet(dataset, point)
			}
		}
		legend = append(legend, lipgloss.NewStyle().Foreground(s.color).Render("● ")+lipgloss.NewStyle().Foreground(p.Mute).Render(s.name))
	}
	chart.SetTimeRange(start, end)
	chart.SetViewTimeRange(start, end)
	chart.SetYRange(low, high)
	chart.SetViewYRange(low, high)
	chart.DrawBrailleAll()
	body := []string{header, strings.TrimRight(chart.View(), "\n")}
	if len(legend) > 1 {
		columnWidth := max(8, (innerWidth-2)/2)
		for i := 0; i < len(legend); i += 2 {
			row := lipgloss.NewStyle().Width(columnWidth).MaxWidth(columnWidth).Render(legend[i])
			if i+1 < len(legend) {
				row = lipgloss.JoinHorizontal(lipgloss.Top, row, "  ", lipgloss.NewStyle().Width(columnWidth).MaxWidth(columnWidth).Render(legend[i+1]))
			}
			body = append(body, row)
		}
	} else {
		body = append(body, legend...)
	}
	return lipgloss.NewStyle().Width(width-2).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Render(strings.Join(body, "\n"))
}

// k8sChartSegments breaks the line where samples are missing for too long.
func k8sChartSegments(metric k8sPodMetric, start, end time.Time, scale float64) [][]timeserieslinechart.TimePoint {
	var points []timeserieslinechart.TimePoint
	for i, at := range metric.times {
		if i < len(metric.values) && !at.Before(start) && !at.After(end) {
			points = append(points, timeserieslinechart.TimePoint{Time: at, Value: metric.values[i] / scale})
		}
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
	var unique []timeserieslinechart.TimePoint
	for _, point := range points {
		if len(unique) > 0 && point.Time.Equal(unique[len(unique)-1].Time) {
			unique[len(unique)-1] = point
		} else {
			unique = append(unique, point)
		}
	}
	if len(unique) == 0 {
		return nil
	}
	var intervals []time.Duration
	for i := 1; i < len(unique); i++ {
		intervals = append(intervals, unique[i].Time.Sub(unique[i-1].Time))
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })
	maxGap := time.Duration(0)
	if len(intervals) > 0 {
		maxGap = 3 * intervals[(len(intervals)-1)/2]
	}
	segments := [][]timeserieslinechart.TimePoint{{unique[0]}}
	for i := 1; i < len(unique); i++ {
		if unique[i].Time.Sub(unique[i-1].Time) > maxGap {
			segments = append(segments, nil)
		}
		last := len(segments) - 1
		segments[last] = append(segments[last], unique[i])
	}
	return segments
}

// fetchK8sDeploymentHTTP reads request, 5xx, and duration signals when available.
func fetchK8sDeploymentHTTP(request k8sDeploymentRequest, samplePod string) (map[string]k8sPodMetric, map[string]k8sPodMetric, map[string]k8sPodMetric) {
	podRequest := k8sPodRequest{
		Profile: request.Profile, Mapping: request.Mapping, Context: request.Context,
		Namespace: request.Namespace, Pod: samplePod, From: request.From,
	}
	catalog, err := fetchK8sPodMetricCatalog(podRequest)
	if err != nil {
		return nil, nil, nil
	}
	countName := catalog.first(
		"http.server.request.duration.count", "http.server.request.duration_count",
		"http_server_request_duration_seconds_count", "http.server.duration.count",
		"http_server_requests_total", "http_requests_total",
		"parseable_incoming_requests", "parseable_incoming_requests_total", "parseable.incoming.requests",
	)
	if countName == "" {
		return nil, nil, nil
	}
	rate := func(selector string) string {
		return "sum by (k8s.pod.name) (rate(" + selector + "[5m]))"
	}
	var requests, errors map[string]k8sPodMetric
	var errorsErr error
	var duration [3]k8sPodMetric
	bucketName := catalog.first(
		"http.server.request.duration.bucket", "http.server.request.duration_bucket",
		"http_server_request_duration_seconds_bucket", "http.server.duration.bucket",
	)
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		requests, _ = fetchK8sDeploymentExpression(request, rate(k8sDeploymentSelector(request, countName)))
	}()
	statusLabel := ""
	for _, label := range []string{"http.response.status_code", "http.status_code", "status_code", "http_status_code", "status"} {
		for _, series := range catalog[countName] {
			if _, ok := series[label]; ok {
				statusLabel = label
				break
			}
		}
		if statusLabel != "" {
			break
		}
	}
	if statusLabel != "" {
		selector := strings.TrimSuffix(k8sDeploymentSelector(request, countName), "}") + "," + statusLabel + `=~"5.."}`
		group.Add(1)
		go func() {
			defer group.Done()
			errors, errorsErr = fetchK8sDeploymentExpression(request, rate(selector))
		}()
	}
	if bucketName != "" {
		for i, quantile := range []string{"0.99", "0.95", "0.50"} {
			group.Add(1)
			go func() {
				defer group.Done()
				expression := fmt.Sprintf("histogram_quantile(%s, sum by (le) (rate(%s[5m])))", quantile, k8sDeploymentSelector(request, bucketName))
				duration[i], _ = fetchK8sPodMetricExpression(podRequest, expression)
				if !strings.Contains(bucketName, "http.server.duration") {
					duration[i] = scaleK8sMetric(duration[i], 1000)
				}
			}()
		}
	}
	group.Wait()
	if len(requests) == 0 {
		return nil, nil, nil
	}
	if statusLabel != "" && errorsErr == nil {
		if errors == nil {
			errors = make(map[string]k8sPodMetric)
		}
		for pod, metric := range requests {
			if _, ok := errors[pod]; !ok {
				errors[pod] = k8sPodMetric{
					times: metric.times, values: make([]float64, len(metric.values)),
					at: metric.at, windowEnd: metric.windowEnd,
				}
			}
		}
	}
	quantiles := make(map[string]k8sPodMetric)
	for i, name := range []string{"p99", "p95", "p50"} {
		if !duration[i].at.IsZero() {
			quantiles[name] = duration[i]
		}
	}
	return requests, errors, quantiles
}

// deploymentRequestSection shows HTTP charts only when request data exists.
func deploymentRequestSection(m k8sDeploymentModel, pods []string, width int) []string {
	if len(m.data.requests) == 0 {
		return nil
	}
	requests := aggregateK8sMetric(m.data.requests, pods, "total")
	if requests.at.IsZero() {
		return nil
	}
	p := ui.Active
	lines := []string{"", lipgloss.NewStyle().Foreground(p.Text).Bold(true).Render("REQUESTS") + "  " + lipgloss.NewStyle().Foreground(p.Mute).Render("HTTP server metrics"), ""}
	type stat struct {
		label, value, unit string
		metric             k8sPodMetric
		color              lipgloss.Color
	}
	stats := []stat{{"Requests per second", fmt.Sprintf("%.2f", requests.latest), "req/s", requests, p.Accent}}
	requestSignals := map[string]k8sPodMetric{"requests": requests}
	requestNames := []string{"requests"}
	if len(m.data.errors) > 0 {
		errors := aggregateK8sMetric(m.data.errors, pods, "total")
		if !errors.at.IsZero() {
			percent := 0.0
			if requests.latest > 0 {
				percent = errors.latest / requests.latest * 100
			}
			stats = append(stats, stat{"5xx errors", fmt.Sprintf("%.2f", percent), "%", errors, p.Err})
			requestSignals["5xx errors"] = errors
			requestNames = append(requestNames, "5xx errors")
		}
	}
	if p95, ok := m.data.duration["p95"]; ok {
		stats = append(stats, stat{"Duration p95", fmt.Sprintf("%.1f", p95.latest), "ms", p95, p.Active})
	}
	columns := make([]string, 0, len(stats))
	columnWidth := max(1, (width-2*(len(stats)-1))/len(stats))
	if width < 90 {
		columnWidth = width
	}
	for _, item := range stats {
		reading := lipgloss.JoinHorizontal(
			lipgloss.Bottom,
			lipgloss.NewStyle().Foreground(item.color).Bold(true).Render(k8sLargeNumber(item.value)),
			" "+lipgloss.NewStyle().Foreground(p.Text).Render(item.unit),
		)
		content := lipgloss.NewStyle().Foreground(p.Mute).Render(item.label) + "\n" + reading
		if len(item.metric.values) > 1 {
			content += "\n\n" + lipgloss.NewStyle().Foreground(item.color).Render(k8sSparkline(item.metric.values, min(36, max(1, columnWidth-2))))
		}
		columns = append(columns, lipgloss.NewStyle().Width(columnWidth).MaxWidth(columnWidth).Render(content))
	}
	if width >= 90 {
		lines = append(lines, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, intersperseK8sColumns(columns)...), "\n")...)
	} else {
		for _, column := range columns {
			lines = append(lines, strings.Split(column, "\n")...)
		}
	}
	lines = append(lines, "")
	chartWidth := width
	if len(m.data.duration) > 0 && width >= 88 {
		chartWidth = (width - 2) / 2
	}
	charts := []string{k8sResourceChart("Requests", requestSignals, requestNames, "total", true, m.request.From, m.data.updatedAt, chartWidth, "req/s", 1, false)}
	if len(m.data.duration) > 0 {
		durationWidth := width
		if width >= 88 {
			durationWidth = width - chartWidth - 2
		}
		charts = append(charts, k8sResourceChart("Duration", m.data.duration, []string{"p99", "p95", "p50"}, "total", true, m.request.From, m.data.updatedAt, durationWidth, "ms", 1, false))
	}
	return appendDeploymentPanels(lines, charts, width)
}

func intersperseK8sColumns(columns []string) []string {
	if len(columns) == 0 {
		return nil
	}
	parts := make([]string, 0, 2*len(columns)-1)
	for i, column := range columns {
		if i > 0 {
			parts = append(parts, "  ")
		}
		parts = append(parts, column)
	}
	return parts
}
