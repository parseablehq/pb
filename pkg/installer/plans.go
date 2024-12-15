package installer

import (
	"fmt"
	"pb/pkg/common"

	"github.com/manifoldco/promptui"
)

type Plan struct {
	Name              string
	IngestionSpeed    string
	PerDayIngestion   string
	QueryPerformance  string
	CPUAndMemorySpecs string
	CPU               int
	Memory            int
}

// Plans define the plans with clear CPU and memory specs for consumption
var Plans = map[string]Plan{
	"Small": {
		Name:              "Small",
		IngestionSpeed:    "1,000 events/sec",
		PerDayIngestion:   "~10GB",
		QueryPerformance:  "Basic performance",
		CPUAndMemorySpecs: "2 CPUs, 4GB RAM",
		CPU:               2,
		Memory:            4,
	},
	"Medium": {
		Name:              "Medium",
		IngestionSpeed:    "10,000 events/sec",
		PerDayIngestion:   "~100GB",
		QueryPerformance:  "Moderate performance",
		CPUAndMemorySpecs: "4 CPUs, 16GB RAM",
		CPU:               4,
		Memory:            16,
	},
	"Large": {
		Name:              "Large",
		IngestionSpeed:    "100,000 events/sec",
		PerDayIngestion:   "~1TB",
		QueryPerformance:  "High performance",
		CPUAndMemorySpecs: "8 CPUs, 32GB RAM",
		CPU:               8,
		Memory:            32,
	},
}

func PromptUserPlanSelection() (Plan, error) {
	planList := []Plan{
		Plans["Small"],
		Plans["Medium"],
		Plans["Large"],
	}

	// Custom template for displaying plans
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▶ {{ .Name | yellow }} ({{ .IngestionSpeed | cyan }})",
		Inactive: "  {{ .Name | yellow }} ({{ .IngestionSpeed | cyan }})",
		Selected: "✔ {{ .Name | green }}",
		Details: `
--------- Plan Details ----------
{{ "Plan:" | faint }}            	{{ .Name }}
{{ "Ingestion Speed:" | faint }} 	{{ .IngestionSpeed }}
{{ "Per Day Ingestion:" | faint }}	{{ .PerDayIngestion }}
{{ "Query Performance:" | faint }}	{{ .QueryPerformance }}
{{ "CPU & Memory:" | faint }}    	{{ .CPUAndMemorySpecs }}`,
	}

	prompt := promptui.Select{
		Label:     fmt.Sprintf(common.Yellow + "Select Deployment Plan"),
		Items:     planList,
		Templates: templates,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return Plan{}, fmt.Errorf("failed to select deployment plan: %w", err)
	}

	return planList[index], nil
}
