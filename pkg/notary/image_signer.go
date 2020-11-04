package notary

import (
	"crypto/tls"
	"encoding/hex"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/buildpacks/lifecycle"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/theupdateframework/notary"
	"github.com/theupdateframework/notary/client"
	"github.com/theupdateframework/notary/client/changelist"
	"github.com/theupdateframework/notary/cryptoservice"
	"github.com/theupdateframework/notary/storage"
	"github.com/theupdateframework/notary/trustmanager"
	"github.com/theupdateframework/notary/trustpinning"
	"github.com/theupdateframework/notary/tuf/data"

	"github.com/pivotal/kpack/pkg/registry"
)

type ImageSigner struct {
	Logger *log.Logger
	Client registry.Client
}

func (s *ImageSigner) Sign(serverURL, notarySecretDir, reportFilePath string, keychain authn.Keychain) error {
	report := lifecycle.ExportReport{}
	_, err := toml.DecodeFile(reportFilePath, &report)
	if err != nil {
		return err
	}

	// TODO : handle all tags
	ref, err := name.ParseReference(report.Image.Tags[0], name.WeakValidation)
	if err != nil {
		return err
	}

	s.Logger.Printf("Pulling image '%s'\n", ref.Context().Name()+"@"+report.Image.Digest)
	image, _, err := s.Client.Fetch(keychain, ref.Context().Name()+"@"+report.Image.Digest)
	if err != nil {
		return err
	}

	imageSize, err := image.Size()
	if err != nil {
		return err
	}

	digestBytes, err := hex.DecodeString(strings.TrimPrefix(report.Image.Digest, "sha256:"))
	if err != nil {
		return err
	}

	// TODO : name should be the tag of the image
	target := &client.Target{
		Name: "latest",
		Hashes: map[string][]byte{
			notary.SHA256: digestBytes,
		},
		Length: imageSize,
	}

	gun := data.GUN(ref.Context().Name())

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // TODO : need to supply ca certs in a config map
		},
	}

	remoteStore, err := storage.NewNotaryServerStore(serverURL, gun, tr)
	if err != nil {
		return err
	}

	clDir, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}

	cl, err := changelist.NewFileChangelist(clDir)
	if err != nil {
		return err
	}

	cryptoStore := storage.NewMemoryStore(nil)

	fileInfos, err := ioutil.ReadDir(notarySecretDir)
	if err != nil {
		return err
	}

	for _, info := range fileInfos {
		if strings.HasSuffix(info.Name(), ".key") {
			buf, err := ioutil.ReadFile(filepath.Join(notarySecretDir, info.Name()))
			if err != nil {
				return err
			}

			err = cryptoStore.Set(strings.TrimSuffix(info.Name(), ".key"), buf)
			if err != nil {
				return err
			}

			break
		}
	}

	cryptoService := cryptoservice.NewCryptoService(trustmanager.NewGenericKeyStore(cryptoStore, k8sSecretPassRetriever(filepath.Join(notarySecretDir, "password"))))

	localStore := storage.NewMemoryStore(nil)

	repo, err := client.NewRepository(
		gun,
		serverURL,
		remoteStore,
		localStore,
		trustpinning.TrustPinConfig{},
		cryptoService,
		cl,
	)
	if err != nil {
		return err
	}

	err = repo.AddTarget(target, data.CanonicalTargetsRole)
	if err != nil {
		return err

	}

	return repo.Publish()
}

func k8sSecretPassRetriever(passwordPath string) func(_, _ string, _ bool, _ int) (passphrase string, giveup bool, err error) {
	return func(_, _ string, _ bool, _ int) (passphrase string, giveup bool, err error) {
		buf, err := ioutil.ReadFile(passwordPath)
		return strings.TrimSpace(string(buf)), false, err
	}
}
