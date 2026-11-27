// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/datasets"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// K8sCmd groups the Kubernetes telemetry commands. It does not contact the Kubernetes API.
var K8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Inspect Kubernetes telemetry stored in Parseable",
}

var k8sSetupCmd = &cobra.Command{
	Use:     "setup",
	Short:   "Connect a Kubernetes context to Parseable telemetry datasets",
	Long:    "Save the metrics dataset for a Kubernetes context. Choose from the datasets in the selected pb profile.",
	Example: "  pb k8s setup --context production\n  pb k8s setup --context production --profile prod --metrics-dataset pai-cluster-metrics",
	Args:    cobra.NoArgs,
	RunE:    runK8sSetup,
}

var k8sPodCmd = &cobra.Command{
	Use:     "pod <name>",
	Short:   "Show a Kubernetes pod's metrics",
	Example: "  pb k8s pod payment-123 -n shop --context production -i",
	Args:    cobra.ExactArgs(1),
	RunE:    runK8sPod,
}

func init() {
	K8sCmd.AddCommand(k8sSetupCmd, k8sPodCmd)
	k8sSetupCmd.Flags().String("context", "", "Kubeconfig context to configure")
	k8sSetupCmd.Flags().String("profile", "", "Saved pb profile (defaults to the active profile)")
	k8sSetupCmd.Flags().String("metrics-dataset", "", "Parseable Kubernetes metrics dataset (prompt on a terminal if omitted)")
	k8sSetupCmd.Flags().String("cluster-name", "", "Value of k8s.cluster.name when multiple clusters share a dataset")
	k8sPodCmd.Flags().String("context", "", "Kubeconfig context configured with pb k8s setup")
	k8sPodCmd.Flags().StringP("namespace", "n", "", "Pod namespace")
	k8sPodCmd.Flags().String("from", "15m", "Look back this far (for example 15m or 1h)")
	k8sPodCmd.Flags().BoolP("interactive", "i", false, "Open interactive pod metrics dashboard")
}

// runK8sSetup saves the Parseable datasets to use for one kubeconfig context.
func runK8sSetup(cmd *cobra.Command, _ []string) error {
	contextName, _ := cmd.Flags().GetString("context")
	profileName, _ := cmd.Flags().GetString("profile")
	metricsDataset, _ := cmd.Flags().GetString("metrics-dataset")
	clusterName, _ := cmd.Flags().GetString("cluster-name")
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return newCLIError(ErrorInvalidInput, "--context is required (use a kubeconfig context name)", nil)
	}
	conf, err := config.ReadConfigFromFile()
	if err != nil {
		return fmt.Errorf("read pb profiles: %w", err)
	}
	if profileName == "" {
		profileName = conf.DefaultProfile
	}
	profile, ok := conf.Profiles[profileName]
	if !ok || profileName == "" {
		return newCLIError(ErrorNotFound, "pb profile not found; run pb login or supply --profile", nil)
	}
	items, err := datasets.FetchHomeDatasets(profile)
	if err != nil {
		return fmt.Errorf("list Parseable datasets: %w", err)
	}
	metricsDataset, err = selectK8sDataset(cmd, items, profileName, datasets.TypeMetrics, metricsDataset)
	if err != nil {
		return err
	}
	// Dataset fetching may refresh Cloud tokens in the saved config.
	conf, err = config.ReadConfigFromFile()
	if err != nil {
		return fmt.Errorf("reload pb profiles: %w", err)
	}
	if conf.K8sContexts == nil {
		conf.K8sContexts = make(map[string]config.K8sContext)
	}
	conf.K8sContexts[contextName] = config.K8sContext{
		Profile: profileName, MetricsDataset: metricsDataset, ClusterName: clusterName,
	}
	if err := config.WriteConfigToFile(conf); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved %s → profile %s\n  Metrics: %s\n", contextName, profileName, metricsDataset)
	return nil
}

// selectK8sDataset uses an explicit name or prompts when running in a terminal.
func selectK8sDataset(cmd *cobra.Command, items []datasets.Dataset, profileName, datasetType, selected string) (string, error) {
	names := datasets.NamesByType(items, datasetType)
	flagName := "--" + datasetType + "-dataset"
	if selected != "" {
		if !slices.Contains(names, selected) {
			return "", newCLIError(ErrorNotFound, fmt.Sprintf("%q is not a %s dataset in profile %q", selected, datasetType, profileName), nil)
		}
		return selected, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", newCLIError(ErrorInvalidInput, flagName+" is required outside an interactive terminal", nil)
	}
	if len(names) == 0 {
		return "", newCLIError(ErrorNotFound, "no "+datasetType+" dataset found in this pb profile", nil)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Which %s dataset contains Kubernetes telemetry for this context?\n", datasetType)
	for i, name := range names {
		fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, name)
	}
	fmt.Fprint(cmd.OutOrStdout(), "Choose number: ")
	var choice string
	if _, err := fmt.Fscanln(cmd.InOrStdin(), &choice); err != nil {
		return "", fmt.Errorf("read %s dataset choice: %w", datasetType, err)
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > len(names) {
		return "", newCLIError(ErrorInvalidInput, "invalid "+datasetType+" dataset choice", nil)
	}
	return names[index-1], nil
}

// runK8sPod opens the metrics dashboard for one pod.
func runK8sPod(cmd *cobra.Command, args []string) error {
	contextName, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	from, _ := cmd.Flags().GetString("from")
	interactive, _ := cmd.Flags().GetBool("interactive")
	if strings.TrimSpace(contextName) == "" || strings.TrimSpace(namespace) == "" {
		return newCLIError(ErrorInvalidInput, "--context and --namespace are required", nil)
	}
	if _, err := parseTimeStr(from); err != nil {
		return newCLIError(ErrorInvalidInput, "invalid --from value", err)
	}
	if !interactive || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return newCLIError(ErrorInvalidInput, "pod dashboard requires -i and a terminal", nil)
	}
	conf, err := config.ReadConfigFromFile()
	if err != nil {
		return err
	}
	mapping, ok := conf.K8sContexts[contextName]
	if !ok {
		return newCLIError(ErrorNotFound, fmt.Sprintf("context %q is not configured; run pb k8s setup --context %s", contextName, contextName), nil)
	}
	profile, ok := conf.Profiles[mapping.Profile]
	if !ok {
		return newCLIError(ErrorNotFound, fmt.Sprintf("pb profile %q no longer exists", mapping.Profile), nil)
	}
	request := k8sPodRequest{Profile: profile, Mapping: mapping, Context: contextName, Namespace: namespace, Pod: args[0], From: from}
	_, err = tea.NewProgram(newK8sPodModel(request), tea.WithAltScreen()).Run()
	return err
}

type k8sPodRequest struct {
	Profile   config.Profile
	client    *internalHTTP.HTTPClient
	Mapping   config.K8sContext
	Context   string
	Namespace string
	Pod       string
	From      string
}

// k8sPodMetric keeps sample times alongside values so charts retain gaps.
type k8sPodMetric struct {
	values    []float64
	times     []time.Time
	latest    float64
	at        time.Time
	windowEnd time.Time
}

func escapeK8sPromQLLabel(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}

func fetchK8sPodMetric(request k8sPodRequest, name string) (k8sPodMetric, error) {
	return fetchK8sPodMetricExpression(request, "sum("+k8sPodSelector(request, name, "")+")")
}

func k8sPodSelector(request k8sPodRequest, name, extraMatcher string) string {
	selector := name + `{k8s.pod.name="` + escapeK8sPromQLLabel(request.Pod) + `",k8s.namespace.name="` + escapeK8sPromQLLabel(request.Namespace) + `"`
	if request.Mapping.ClusterName != "" {
		selector += `,k8s.cluster.name="` + escapeK8sPromQLLabel(request.Mapping.ClusterName) + `"`
	}
	if extraMatcher != "" {
		selector += "," + extraMatcher
	}
	return selector + "}"
}

// fetchK8sPodMetricExpression reads one pod's PromQL series from Parseable.
func fetchK8sPodMetricExpression(request k8sPodRequest, expression string) (k8sPodMetric, error) {
	var metric k8sPodMetric
	start, end, err := k8sMetricWindow(request.From)
	if err != nil {
		return metric, err
	}
	metric.windowEnd = end
	params := url.Values{}
	params.Set("query", expression)
	params.Set("stream", request.Mapping.MetricsDataset)
	params.Set("start", start.UTC().Format(time.RFC3339))
	params.Set("end", end.Format(time.RFC3339))
	params.Set("step", strconv.Itoa(k8sMetricStep(start, end)))
	endpoint, err := url.JoinPath(request.Profile.URL, "prometheus/api/v1/query_range")
	if err != nil {
		return metric, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return metric, err
	}
	resp, err := k8sMetricsClient(request.Profile, request.client).DoAuthenticated(req)
	if err != nil {
		return metric, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := readLimitedErrorPreview(resp.Body)
		return metric, fmt.Errorf("metrics query failed: %s: %s", resp.Status, preview)
	}
	var result promqlResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return metric, fmt.Errorf("decode pod metrics: %w", err)
	}
	if result.Status == "error" {
		return metric, fmt.Errorf("metrics query failed: %s", result.Error)
	}
	for _, series := range result.Data.Result {
		candidate := k8sPodMetric{windowEnd: end}
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
			candidate.values = append(candidate.values, value)
			at := time.Unix(int64(stamp), 0)
			candidate.times = append(candidate.times, at)
			if at.After(candidate.at) {
				candidate.at, candidate.latest = at, value
			}
		}
		if candidate.at.After(metric.at) {
			metric = candidate
		}
	}
	return metric, nil
}

func cleanK8sText(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] == 0x1b && i+1 < len(value) {
			switch value[i+1] {
			case '[': // ANSI control sequence, such as a color or cursor command.
				i += 2
				for i < len(value) && (value[i] < 0x40 || value[i] > 0x7e) {
					i++
				}
				if i < len(value) {
					i++
				}
				continue
			case ']': // OSC sequence, terminated by BEL or ESC backslash.
				i += 2
				for i < len(value) && value[i] != 0x07 {
					if value[i] == 0x1b && i+1 < len(value) && value[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				if i < len(value) && value[i] == 0x07 {
					i++
				}
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if r < 32 || r == 127 {
			out.WriteByte(' ')
		} else {
			out.WriteRune(r)
		}
		i += size
	}
	return out.String()
}

type k8sPodLoadedMsg struct {
	loadID    uint64
	cpu       k8sPodMetric
	memory    k8sPodMetric
	extra     k8sPodExtraMetrics
	metricErr error
}

type k8sRefreshMsg time.Time

func k8sRefreshTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(at time.Time) tea.Msg { return k8sRefreshMsg(at) })
}

// k8sPodModel holds the current pod readings and terminal navigation state.
type k8sPodModel struct {
	request   k8sPodRequest
	loadID    uint64
	loading   bool
	metricErr error
	cpu       k8sPodMetric
	memory    k8sPodMetric
	extra     k8sPodExtraMetrics
	width     int
	height    int
	scroll    int
	updatedAt time.Time
}

func newK8sPodModel(request k8sPodRequest) k8sPodModel {
	request.client = k8sMetricsClient(request.Profile, request.client)
	return k8sPodModel{request: request, loadID: 1, loading: true, width: 100, height: 25}
}

func (m k8sPodModel) Init() tea.Cmd { return tea.Batch(m.load(), k8sRefreshTick()) }

func (m k8sPodModel) load() tea.Cmd {
	request := m.request
	loadID := m.loadID
	return func() tea.Msg {
		var cpu, memory k8sPodMetric
		var extra k8sPodExtraMetrics
		var cpuErr, memoryErr, extraErr error
		var group sync.WaitGroup
		group.Add(3)
		go func() {
			defer group.Done()
			cpu, cpuErr = fetchK8sPodMetric(request, "k8s.pod.cpu.usage")
		}()
		go func() {
			defer group.Done()
			memory, memoryErr = fetchK8sPodMetric(request, "k8s.pod.memory.usage")
		}()
		go func() {
			defer group.Done()
			extra, extraErr = fetchK8sPodExtras(request)
		}()
		group.Wait()
		return k8sPodLoadedMsg{loadID: loadID, cpu: cpu, memory: memory, extra: extra, metricErr: errors.Join(cpuErr, memoryErr, extraErr)}
	}
}

func (m k8sPodModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(20, msg.Width), max(6, msg.Height)
	case k8sPodLoadedMsg:
		if msg.loadID != m.loadID {
			return m, nil
		}
		m.loading = false
		m.cpu, m.memory, m.extra, m.metricErr = msg.cpu, msg.memory, msg.extra, msg.metricErr
		m.updatedAt = time.Now()
	case k8sRefreshMsg:
		if m.loading {
			return m, k8sRefreshTick()
		}
		m.loading = true
		m.loadID++
		return m, tea.Batch(m.load(), k8sRefreshTick())
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		case "pgup", "ctrl+b":
			m.scroll = max(0, m.scroll-max(1, m.height-7))
		case "pgdown", "ctrl+f":
			m.scroll += max(1, m.height-7)
		case "r":
			m.loading, m.metricErr = true, nil
			m.loadID++
			return m, m.load()
		case "1", "2", "3", "4":
			m.request.From = map[string]string{"1": "15m", "2": "1h", "3": "6h", "4": "24h"}[msg.String()]
			m.loading, m.metricErr, m.scroll = true, nil, 0
			m.loadID++
			return m, m.load()
		}
	}
	return m, nil
}

func k8sSparkline(values []float64, width int) string {
	if len(values) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	low, high := values[0], values[0]
	for _, value := range values {
		low = min(low, value)
		high = max(high, value)
	}
	count := min(len(values), width)
	var out strings.Builder
	for i := 0; i < count; i++ {
		index := len(values) - 1
		if count > 1 {
			index = i * (len(values) - 1) / (count - 1)
		}
		value := values[index]
		level := 3
		if high > low {
			level = int(math.Round((value - low) / (high - low) * 7))
		}
		out.WriteRune(blocks[level])
	}
	return out.String()
}

func truncateK8sLine(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}

var _ tea.Model = k8sPodModel{}

// View renders a scrollable pod dashboard. All values come from Parseable.
func (m k8sPodModel) View() string {
	p := ui.Active
	outerWidth := max(30, m.width)
	margin := 1
	if outerWidth >= 60 {
		margin = 2
	}
	width := outerWidth - margin*2
	muted := lipgloss.NewStyle().Foreground(p.Mute)
	strong := lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	title := muted.Render("POD  ") + strong.Render(truncateK8sLine(m.request.Namespace+"/"+m.request.Pod, max(1, width-7)))
	updated := "loading"
	if !m.updatedAt.IsZero() {
		updated = m.updatedAt.Local().Format("15:04:05 MST")
	}
	brand := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("PB  /  KUBERNETES")
	kind := muted.Render("POD MONITOR")
	if gap := width - lipgloss.Width(brand) - lipgloss.Width(kind) - 4; gap >= 2 {
		brand += strings.Repeat(" ", gap) + kind + "  "
	}
	header := []string{
		brand,
		"",
		title,
	}
	header = append(header, k8sPodMetadata(width, "PARSEABLE PROFILE", m.request.Mapping.Profile, "KUBE CONTEXT", m.request.Context)...)
	header = append(header, k8sPodMetadata(width, "NAMESPACE", m.request.Namespace, "UPDATED", updated+"  ·  refresh every 15s")...)
	header = append(header, "")
	header = append(header, strings.Split(podControls(m), "\n")...)
	header = append(header, "")
	var body []string
	if m.loading && m.updatedAt.IsZero() {
		body = append(body, muted.Render("Loading pod telemetry..."))
	} else {
		cardWidth := width
		if width >= 88 {
			cardWidth = (width - 2) / 2
		}
		var summaries, charts []string
		pod := m.request.Pod
		if !m.cpu.at.IsZero() {
			summaries = append(summaries, deploymentSummary("CPU", m.cpu, m.extra.cpuRequest, m.extra.cpuLimit, true, cardWidth))
			charts = append(charts, podChart("CPU", m.cpu, pod, m.request.From, m.updatedAt, cardWidth, true))
		}
		if !m.memory.at.IsZero() {
			summaries = append(summaries, deploymentSummary("Memory", m.memory, m.extra.memoryRequest, m.extra.memoryLimit, false, cardWidth))
			charts = append(charts, podChart("Memory", m.memory, pod, m.request.From, m.updatedAt, cardWidth, false))
		}
		body = appendDeploymentPanels(body, summaries, width)
		body = append(body, "")
		body = appendDeploymentPanels(body, charts, width)
		if len(charts) == 0 {
			body = append(body, muted.Render("No pod metrics in this time range."))
		}
		if m.metricErr != nil {
			body = append(body, lipgloss.NewStyle().Foreground(p.Err).Render(truncateK8sLine("Metrics: "+cleanK8sText(m.metricErr.Error()), width)))
		}
		if !m.extra.requests.at.IsZero() || !m.extra.errors.at.IsZero() || !m.extra.latency.at.IsZero() {
			body = append(body, "", lipgloss.NewStyle().Foreground(p.Text).Bold(true).Render("Requests"), "")
			var requestCards []string
			if !m.extra.requests.at.IsZero() {
				requestCards = append(requestCards, k8sSignalCard("Requests/s", m.extra.requests, cardWidth, m.request.From, m.updatedAt, "req/s"))
			}
			if !m.extra.errors.at.IsZero() {
				requestCards = append(requestCards, k8sSignalCard("5xx errors/s", m.extra.errors, cardWidth, m.request.From, m.updatedAt, "err/s"))
			}
			if !m.extra.latency.at.IsZero() {
				requestCards = append(requestCards, k8sSignalCard(m.extra.latencyTitle, m.extra.latency, cardWidth, m.request.From, m.updatedAt, "ms"))
			}
			body = appendK8sCards(body, requestCards, width)
		}
	}
	footerText := "1–4 range   ↑↓ scroll   r refresh   q back"
	footer := muted.Render(truncateK8sLine(footerText, width))
	height := max(6, m.height)
	if len(header) > height-2 {
		header = header[:height-2]
	}
	visible := max(0, height-len(header)-2)
	maxScroll := max(0, len(body)-visible)
	scroll := min(m.scroll, maxScroll)
	end := min(len(body), scroll+visible)
	window := append(append([]string{}, header...), body[scroll:end]...)
	for len(window) < height-2 {
		window = append(window, "")
	}
	window = append(window, "", footer)
	return lipgloss.NewStyle().Width(outerWidth).Padding(0, margin).Render(strings.Join(window, "\n"))
}

func k8sPodMetadata(width int, firstLabel, firstValue, secondLabel, secondValue string) []string {
	p := ui.Active
	label := lipgloss.NewStyle().Foreground(p.Mute)
	value := lipgloss.NewStyle().Foreground(p.Text)
	render := func(name, text string) string {
		if text == "" {
			text = "—"
		}
		prefix := name + "  "
		return label.Render(prefix) + value.Render(truncateK8sLine(text, max(1, width-lipgloss.Width(prefix)-2)))
	}
	first := render(firstLabel, firstValue)
	second := render(secondLabel, secondValue)
	if lipgloss.Width(first)+lipgloss.Width(second)+4 <= width-2 {
		return []string{first + "    " + second}
	}
	return []string{first, second}
}

func podControls(m k8sPodModel) string {
	var ranges []string
	for _, span := range []string{"15m", "1h", "6h", "24h"} {
		ranges = append(ranges, deploymentButton(span, m.request.From == span))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, ranges[0], " ", ranges[1], " ", ranges[2], " ", ranges[3])
}

func k8sSignalCard(name string, metric k8sPodMetric, width int, span string, end time.Time, unit string) string {
	title := fmt.Sprintf("%s · %.2f %s", name, metric.latest, unit)
	return k8sResourceChart(title, map[string]k8sPodMetric{"signal": metric}, []string{"signal"}, "total", true, span, end, width, unit, 1, false)
}

func appendK8sCards(body, cards []string, width int) []string {
	for i := 0; i < len(cards); {
		if width >= 88 && i+1 < len(cards) {
			body = append(body, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, cards[i], "  ", cards[i+1]), "\n")...)
			i += 2
		} else {
			body = append(body, strings.Split(cards[i], "\n")...)
			i++
		}
	}
	return body
}
