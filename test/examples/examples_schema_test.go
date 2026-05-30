package examples_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	hermesv1 "github.com/paperclipinc/hermes-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func TestExamplesStrictlyDecodeHermesCRs(t *testing.T) {
	examplesDir := filepath.Join(repoRoot(t), "examples")

	scheme := kruntime.NewScheme()
	if err := hermesv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register hermes scheme: %v", err)
	}
	decoder := serializer.NewCodecFactory(scheme, serializer.EnableStrict).UniversalDeserializer()

	decoded := 0
	err := filepath.WalkDir(examplesDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isYAML(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}

		reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for docIndex := 1; ; docIndex++ {
			doc, err := reader.Read()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				t.Errorf("%s document %d: read yaml: %v", relPath, docIndex, err)
				continue
			}
			if len(bytes.TrimSpace(doc)) == 0 {
				continue
			}

			var typeMeta metav1.TypeMeta
			if err := yaml.Unmarshal(doc, &typeMeta); err != nil {
				t.Errorf("%s document %d: decode type metadata: %v", relPath, docIndex, err)
				continue
			}
			if typeMeta.APIVersion != hermesv1.GroupVersion.String() {
				continue
			}
			if !isHermesV1Kind(typeMeta.Kind) {
				t.Errorf("%s document %d: unsupported Hermes kind %q", relPath, docIndex, typeMeta.Kind)
				continue
			}

			obj, gvk, err := decoder.Decode(doc, nil, nil)
			if err != nil {
				t.Errorf("%s document %d: strict decode %s/%s: %v", relPath, docIndex, typeMeta.APIVersion, typeMeta.Kind, err)
				continue
			}
			if gvk.GroupVersion().String() != hermesv1.GroupVersion.String() || gvk.Kind != typeMeta.Kind {
				t.Errorf("%s document %d: decoded as %s, want %s/%s", relPath, docIndex, gvk.String(), typeMeta.APIVersion, typeMeta.Kind)
				continue
			}
			if !matchesHermesV1Kind(typeMeta.Kind, obj) {
				t.Errorf("%s document %d: decoded %s into %T", relPath, docIndex, typeMeta.Kind, obj)
				continue
			}
			decoded++
		}
	})
	if err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	if decoded == 0 {
		t.Fatal("no Hermes CR YAML found under examples")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func isHermesV1Kind(kind string) bool {
	switch kind {
	case "HermesClusterDefaults", "HermesInstance", "HermesSelfConfig":
		return true
	default:
		return false
	}
}

func matchesHermesV1Kind(kind string, obj kruntime.Object) bool {
	switch kind {
	case "HermesClusterDefaults":
		_, ok := obj.(*hermesv1.HermesClusterDefaults)
		return ok
	case "HermesInstance":
		_, ok := obj.(*hermesv1.HermesInstance)
		return ok
	case "HermesSelfConfig":
		_, ok := obj.(*hermesv1.HermesSelfConfig)
		return ok
	default:
		return false
	}
}
