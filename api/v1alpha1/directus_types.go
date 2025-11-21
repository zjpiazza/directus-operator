/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DirectusSpec defines the desired state of Directus
type DirectusSpec struct {
	// Image is the Directus image to use.
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the number of replicas.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Database configures the database connection.
	// +optional
	Database DatabaseConfig `json:"database,omitempty"`

	// Extensions is a list of extensions to install.
	// +optional
	Extensions []Extension `json:"extensions,omitempty"`

	// ExtensionsConfig configures extension installation.
	// +optional
	ExtensionsConfig *ExtensionsConfig `json:"extensionsConfig,omitempty"`

	// Ingress configures the ingress.
	// +optional
	Ingress IngressConfig `json:"ingress,omitempty"`

	// PublicURL overrides the PUBLIC_URL environment variable.
	// +optional
	PublicURL string `json:"publicUrl,omitempty"`
}

type DatabaseConfig struct {
	// Type is the database type (postgres, mysql).
	// +optional
	Type string `json:"type,omitempty"`

	// Postgres configures the CloudNativePG cluster if Type is postgres.
	// +optional
	Postgres *PostgresConfig `json:"postgres,omitempty"`
}

type PostgresConfig struct {
	// Instances is the number of Postgres instances.
	// +optional
	Instances int `json:"instances,omitempty"`

	// Storage is the storage size.
	// +optional
	Storage string `json:"storage,omitempty"`
}

type Extension struct {
	// Name is the name of the extension.
	Name string `json:"name"`

	// Source is the URL to download the extension from (git or http).
	Source string `json:"source"`

	// Version is the version of the extension to install (npm only).
	// +optional
	Version string `json:"version,omitempty"`

	// Type is the type of extension (interface, layout, etc.).
	// +optional
	Type string `json:"type,omitempty"`
}

type ExtensionsConfig struct {
	// SecretRef is the name of the secret containing the .npmrc file.
	// The secret must have a key named ".npmrc".
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

type IngressConfig struct {
	// Enabled enables ingress.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the ingress host.
	// +optional
	Host string `json:"host,omitempty"`

	// TLS enables TLS.
	// +optional
	TLS bool `json:"tls,omitempty"`
}

// DirectusStatus defines the observed state of Directus
type DirectusStatus struct {
	// DatabaseReady indicates if the database is ready.
	DatabaseReady bool `json:"databaseReady,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Directus is the Schema for the directuses API
type Directus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DirectusSpec   `json:"spec,omitempty"`
	Status DirectusStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DirectusList contains a list of Directus
type DirectusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Directus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Directus{}, &DirectusList{})
}
