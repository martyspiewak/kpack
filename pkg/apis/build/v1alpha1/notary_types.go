package v1alpha1

type NotaryConfig struct {
	V1 *NotaryV1Config `json:"v1,omitempty"`
}

type NotaryV1Config struct {
	URL       string          `json:"url"`
	SecretRef NotarySecretRef `json:"secretRef"`
}

type NotarySecretRef struct {
	Name string `json:"name"`
}
