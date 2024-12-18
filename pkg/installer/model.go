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

// deploymentType represents the type of deployment for the application.
type deploymentType string

const (
	// standalone is a single-node deployment.
	standalone deploymentType = "standalone"
	// distributed is a multi-node deployment.
	distributed deploymentType = "distributed"
)

// loggingAgent represents the type of logging agent used.
type loggingAgent string

const (
	// fluentbit specifies Fluent Bit as the logging agent.
	fluentbit loggingAgent = "fluentbit"
	// vector specifies Vector as the logging agent.
	vector loggingAgent = "vector"
	// none specifies no logging agent or a custom logging agent.
	_ loggingAgent = "I have my agent running / I'll set up later"
)

// ParseableInfo represents the info used to authenticate, metadata with Parseable.
type ParseableInfo struct {
	Name      string // Name for parseable
	Namespace string // Namespace for parseable
	Username  string // Username for authentication.
	Password  string // Password for authentication.
}

// ObjectStore represents the type of object storage backend.
type ObjectStore string

const (
	// S3Store represents an S3-compatible object store.
	S3Store ObjectStore = "s3-store"
	// LocalStore represents a local file system storage backend.
	LocalStore ObjectStore = "local-store"
	// BlobStore represents an Azure Blob Storage backend.
	BlobStore ObjectStore = "blob-store"
	// GcsStore represents a Google Cloud Storage backend.
	GcsStore ObjectStore = "gcs-store"
)

// ObjectStoreConfig contains the configuration for the object storage backend.
type ObjectStoreConfig struct {
	StorageClass string      // Storage class of the object store.
	ObjectStore  ObjectStore // Type of object store being used.
	S3Store      S3          // S3-specific configuration.
	BlobStore    Blob        // Azure Blob-specific configuration.
	GCSStore     GCS         // GCS-specific configuration.
}

// S3 contains configuration details for an S3-compatible object store.
type S3 struct {
	URL       string // URL of the S3-compatible object store.
	AccessKey string // Access key for authentication.
	SecretKey string // Secret key for authentication.
	Bucket    string // Bucket name in the S3 store.
	Region    string // Region of the S3 store.
}

// GCS contains configuration details for a Google Cloud Storage backend.
type GCS struct {
	URL       string // URL of the GCS-compatible object store.
	AccessKey string // Access key for authentication.
	SecretKey string // Secret key for authentication.
	Bucket    string // Bucket name in the GCS store.
	Region    string // Region of the GCS store.
}

// Blob contains configuration details for an Azure Blob Storage backend.
type Blob struct {
	AccessKey   string // Access key for authentication.
	AccountName string // Account name for Azure Blob Storage.
	Container   string // Container name in the Azure Blob store.
	URL         string // URL of the Azure Blob store.
}
