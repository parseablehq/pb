// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

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
	CPU               string
	Memory            string
	Mode              string
	Description       string
}

// Plans define the plans with clear CPU and memory specs for consumption
var Plans = map[string]Plan{
	"Playground": {
		Name:              "Playground",
		Description:       "Suitable for testing and PoC",
		IngestionSpeed:    "Up to 5 MiB/sec",
		CPUAndMemorySpecs: "1 vCPU, 1Gi RAM",
		CPU:               "1",
		Memory:            "1Gi",
		Mode:              "Standalone",
	},
	"Small": {
		Name:              "Small",
		Description:       "Suitable for production grade, small volume workloads",
		IngestionSpeed:    "Up to 20 MiB/sec",
		CPUAndMemorySpecs: "2 vCPUs, 4Gi RAM",
		CPU:               "2",
		Memory:            "4Gi",
		Mode:              "Distributed (1 Query pod, 3 Ingest pod)",
	},
	"Medium": {
		Name:              "Medium",
		IngestionSpeed:    "Up to 50 MiB/sec",
		CPUAndMemorySpecs: "4 vCPUs, 16Gi RAM",
		CPU:               "4",
		Memory:            "18Gi",
		Mode:              "Distributed (1 Query pod, 3 Ingest pod)",
	},
	"Large": {
		Name:              "Large",
		IngestionSpeed:    "Up to 100 MiB/sec",
		CPUAndMemorySpecs: "8 vCPUs, 32Gi RAM",
		CPU:               "8",
		Memory:            "16Gi",
		Mode:              "Distributed (1 Query pod, 3 Ingest pod)",
	},
}

func promptUserPlanSelection() (Plan, error) {
	planList := []Plan{
		Plans["Playground"],
		Plans["Small"],
		Plans["Medium"],
		Plans["Large"],
	}

	// Custom template for displaying plans
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▶ {{ .Name | yellow }} ",
		Inactive: "  {{ .Name | yellow }} ",
		Selected: "{{ `Selected plan:` | green }} '{{ .Name | green }}' ✔ ",
		Details: `
--------- Plan Details ----------
{{ "Plan:" | faint }}            	{{ .Name }}
{{ "Ingestion Speed:" | faint }} 	{{ .IngestionSpeed }}
{{ "Infrastructure:" | faint }} 	{{ .Mode }}
{{ "CPU & Memory:" | faint }}    	{{ .CPUAndMemorySpecs }} per pod`,
	}

	// Add a note about the default plan in the label
	label := fmt.Sprintf(common.Yellow + "Select deployment type:")

	prompt := promptui.Select{
		Label:     label,
		Items:     planList,
		Templates: templates,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return Plan{}, fmt.Errorf("failed to select deployment type: %w", err)
	}

	selectedPlan := planList[index]
	fmt.Printf(
		common.Cyan+"  Ingestion Speed: %s\n"+
			common.Cyan+"  Per Day Ingestion: %s\n"+
			common.Cyan+"  Query Performance: %s\n"+
			common.Cyan+"  CPU & Memory: %s\n"+
			common.Reset, selectedPlan.IngestionSpeed, selectedPlan.PerDayIngestion,
		selectedPlan.QueryPerformance, selectedPlan.CPUAndMemorySpecs)

	return selectedPlan, nil
}
