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

package cmd

import (
	"log"
	"pb/pkg/helm"
	"sync"

	"github.com/spf13/cobra"
)

var GenerateK8sCmd = &cobra.Command{
	Use:     "k8s-events",
	Short:   "Generate k8s events on your k8s cluster by deploying apps in different states.",
	Example: "pb generate k8s events",
	RunE: func(cmd *cobra.Command, args []string) error {
		apps := []helm.Helm{
			{
				ReleaseName: "ingress-nginx",
				Namespace:   "pb-ingress",
				RepoName:    "ingress-nginx",
				RepoUrl:     "https://kubernetes.github.io/ingress-nginx",
				ChartName:   "ingress-nginx",
				Version:     "4.0.3",
			},
			{
				ReleaseName: "prometheus",
				Namespace:   "pb-monitoring",
				RepoName:    "prometheus-community",
				RepoUrl:     "https://prometheus-community.github.io/helm-charts",
				ChartName:   "prometheus",
				Version:     "15.0.0",
			},
			{
				ReleaseName: "grafana",
				Namespace:   "pb-grafana",
				RepoName:    "grafana",
				RepoUrl:     "https://grafana.github.io/helm-charts",
				ChartName:   "grafana",
				Version:     "6.16.0",
			},
			{
				ReleaseName: "postgres",
				Namespace:   "pb-db",
				RepoName:    "bitnami",
				RepoUrl:     "https://charts.bitnami.com/bitnami",
				ChartName:   "postgresql",
				Version:     "11.6.15",
			},
		}

		// Create a WaitGroup to manage Go routines
		var wg sync.WaitGroup

		// Use a channel to capture errors from Go routines
		errCh := make(chan error, len(apps))

		for _, app := range apps {
			wg.Add(1)
			go func(app helm.Helm) {
				defer wg.Done() // Mark this Go routine as done when it finishes
				log.Printf("Deploying %s...", app.ReleaseName)
				if err := helm.Apply(app); err != nil {
					log.Printf("Failed to deploy %s: %v", app.ReleaseName, err)
					errCh <- err // Send the error to the channel
					return
				}
				log.Printf("%s deployed successfully.", app.ReleaseName)
			}(app) // Pass the app variable to the closure to avoid capturing issues
		}

		// Wait for all Go routines to complete
		wg.Wait()
		close(errCh) // Close the error channel after all routines finish

		// Check for errors from Go routines
		for err := range errCh {
			if err != nil {
				return err // Return the first error encountered
			}
		}

		log.Println("All applications deployed successfully.")
		return nil
	},
}

var GenerateK8sUninstallCmd = &cobra.Command{
	Use:     "k8s-uninstall",
	Short:   "Uninstall Helm releases and generate Kubernetes events.",
	Example: "pb generate k8s uninstall",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		apps := []helm.Helm{
			{
				ReleaseName: "ingress-nginx",
				Namespace:   "pb-ingress",
			},
			{
				ReleaseName: "prometheus",
				Namespace:   "pb-monitoring",
			},
			{
				ReleaseName: "grafana",
				Namespace:   "pb-grafana",
			},
			{
				ReleaseName: "postgres",
				Namespace:   "pb-db",
			},
		}

		for _, app := range apps {
			log.Printf("Uninstalling %s...", app.ReleaseName)
			if err := helm.DeleteRelease(app.ReleaseName, app.Namespace); err != nil {
				log.Printf("Failed to uninstall %s: %v", app.ReleaseName, err)
				return err
			}
			log.Printf("%s uninstalled successfully.", app.ReleaseName)
		}
		return nil
	},
}
