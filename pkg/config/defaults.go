// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package config

import "fmt"

// CloudOrchestratorURL and CloudOrchestratorAuthToken are populated in release
// binaries through Go's -X linker flag.
var (
	CloudOrchestratorURL       string
	CloudOrchestratorAuthToken string
)

// Service and user-facing URL defaults used by pb.
const (
	// Self-hosted defaults used by self-hosted login (placeholder values).
	LocalParseableURL = "http://localhost:8000"

	// Analytics is retained in the codebase but disabled for this release by
	// analyticsEnabled in main.go.
	AnalyticsURL = "https://analytics.parseable.io:80/pb"

	// Installer defaults are retained for future development. The cluster
	// install/uninstall commands are not registered in this release, so these
	// values are not reachable through the released CLI.
	HelmChartRepositoryURL  = "https://charts.parseable.com"
	GoogleCloudStorageURL   = "https://storage.googleapis.com"
	DocumentationURL        = "https://www.parseable.com/docs/server/introduction"
	StreamManagementDocsURL = "https://www.parseable.com/docs/server/api"
)

// AmazonS3URL is used only by the currently unregistered cluster installer.
func AmazonS3URL(region string) string {
	return fmt.Sprintf("https://s3.%s.amazonaws.com", region)
}

// AzureBlobStorageURL is used only by the currently unregistered cluster installer.
func AzureBlobStorageURL(storageAccount string) string {
	return fmt.Sprintf("https://%s.blob.core.windows.net", storageAccount)
}
