//go:build helm

package helm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderContracts(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")
	output, err := renderChart(chartPath)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, output)
	}

	rendered := string(output)
	serverDeployment := findDocument(t, rendered, "Deployment", "server")
	workerDeployment := findDocument(t, rendered, "Deployment", "worker")
	serverMonitor := findDocument(t, rendered, "ServiceMonitor", "gophprofile-server")
	workerMonitor := findDocument(t, rendered, "ServiceMonitor", "gophprofile-worker")
	ingress := findDocument(t, rendered, "Ingress", "server")
	migrateJob := findDocument(t, rendered, "Job", "migrate")

	assertContains(t, serverDeployment, "checksum/config:")
	assertContains(t, serverDeployment, "checksum/secret:")
	assertContains(t, serverDeployment, "path: /live")
	assertContains(t, serverDeployment, "path: /health")
	assertContains(t, serverDeployment, "preStop:")
	assertContains(t, serverDeployment, "seconds: 5")
	assertContains(t, serverDeployment, "name: wait-for-migrations")
	assertContains(t, serverDeployment, "schema_migrations")
	assertNotContains(t, serverDeployment, "\n  replicas:")

	assertContains(t, workerDeployment, "checksum/config:")
	assertContains(t, workerDeployment, "checksum/secret:")
	assertContains(t, workerDeployment, "name: wait-for-migrations")
	assertContains(t, workerDeployment, "schema_migrations")
	assertContains(t, workerDeployment, "path: /live")
	assertContains(t, workerDeployment, "path: /health")

	assertContains(t, serverMonitor, "port: http")
	assertContains(t, serverMonitor, "path: /metrics")
	assertContains(t, workerMonitor, "port: metrics")
	assertContains(t, workerMonitor, "path: /metrics")
	assertNotContains(t, ingress, "path: /metrics")
	assertContains(t, migrateJob, `"helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded`)
	assertContains(t, migrateJob, "secretKeyRef:")

	if strings.Contains(rendered, "kind: Secret") {
		t.Fatal("default values must not render Kubernetes Secret resources")
	}
}

func TestConfigChangeUpdatesPodTemplateChecksum(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")

	defaultOutput, err := renderChart(chartPath)
	if err != nil {
		t.Fatalf("helm template with default config: %v\n%s", err, defaultOutput)
	}

	changedOutput, err := renderChart(chartPath, "--set", "config.logLevel=debug")
	if err != nil {
		t.Fatalf("helm template with changed config: %v\n%s", err, changedOutput)
	}

	for _, name := range []string{"server", "worker"} {
		defaultDeployment := findDocument(t, string(defaultOutput), "Deployment", name)
		changedDeployment := findDocument(t, string(changedOutput), "Deployment", name)

		defaultChecksum := findFieldValue(t, defaultDeployment, "checksum/config:")
		changedChecksum := findFieldValue(t, changedDeployment, "checksum/config:")
		if defaultChecksum == changedChecksum {
			t.Fatalf("%s Deployment config checksum did not change", name)
		}
	}
}

func TestManagedSecretChangeUpdatesPodTemplateChecksum(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")
	baseArgs := []string{
		"--set", "secrets.create=true",
		"--set", "secrets.databaseDsn=postgres://gophprofile:old@postgres:5432/gophprofile",
		"--set", "secrets.s3AccessKey=old-access",
		"--set", "secrets.s3SecretKey=old-secret",
		"--set", "secrets.rabbitmqUrl=amqp://gophprofile:old@rabbitmq:5672/",
	}

	defaultOutput, err := renderChart(chartPath, baseArgs...)
	if err != nil {
		t.Fatalf("helm template with managed secrets: %v\n%s", err, defaultOutput)
	}

	changedArgs := append([]string{}, baseArgs...)
	changedArgs = append(changedArgs, "--set", "secrets.rabbitmqUrl=amqp://gophprofile:new@rabbitmq:5672/")
	changedOutput, err := renderChart(chartPath, changedArgs...)
	if err != nil {
		t.Fatalf("helm template with changed managed secret: %v\n%s", err, changedOutput)
	}

	for _, name := range []string{"server", "worker"} {
		defaultDeployment := findDocument(t, string(defaultOutput), "Deployment", name)
		changedDeployment := findDocument(t, string(changedOutput), "Deployment", name)

		defaultChecksum := findFieldValue(t, defaultDeployment, "checksum/secret:")
		changedChecksum := findFieldValue(t, changedDeployment, "checksum/secret:")
		if defaultChecksum == changedChecksum {
			t.Fatalf("%s Deployment managed secret checksum did not change", name)
		}
	}
}

func TestMigrationKeepsManagedDatabaseDsnInSecretReference(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")
	wantDsn := "postgres://gophprofile:new@postgres:5432/gophprofile"

	output, err := renderChart(
		chartPath,
		"--set", "secrets.create=true",
		"--set", "secrets.databaseDsn="+wantDsn,
		"--set", "secrets.s3AccessKey=access",
		"--set", "secrets.s3SecretKey=secret",
		"--set", "secrets.rabbitmqUrl=amqp://gophprofile:password@rabbitmq:5672/",
	)
	if err != nil {
		t.Fatalf("helm template with managed secrets: %v\n%s", err, output)
	}

	migrateJob := findDocument(t, string(output), "Job", "migrate")
	assertContains(t, migrateJob, "secretKeyRef:")
	assertContains(t, migrateJob, "name: gophprofile-secrets")
	assertContains(t, migrateJob, "key: DATABASE_DSN")
	assertNotContains(t, migrateJob, wantDsn)
}

func TestManagedDatabaseDsnUpgradeRequiresExistingSecret(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")

	output, err := renderChart(
		chartPath,
		"--is-upgrade",
		"--set", "secrets.create=true",
		"--set", "secrets.databaseDsn=postgres://gophprofile:password@postgres:5432/gophprofile",
		"--set", "secrets.s3AccessKey=access",
		"--set", "secrets.s3SecretKey=secret",
		"--set", "secrets.rabbitmqUrl=amqp://gophprofile:password@rabbitmq:5672/",
	)
	if err == nil {
		t.Fatal("helm template --is-upgrade with managed secrets must require the existing Secret")
	}

	assertContains(t, string(output), `managed Secret "gophprofile-secrets" must already exist`)
}

func TestLocalValues(t *testing.T) {
	chartPath := filepath.Join("..", "..", "deploy", "helm", "gophprofile")
	valuesPath := filepath.Join(chartPath, "values-local.yaml")

	output, err := renderChart(chartPath, "-f", valuesPath)
	if err != nil {
		t.Fatalf("helm template with local values: %v\n%s", err, output)
	}

	rendered := string(output)
	serverDeployment := findDocument(t, rendered, "Deployment", "server")
	workerDeployment := findDocument(t, rendered, "Deployment", "worker")
	migrateJob := findDocument(t, rendered, "Job", "migrate")

	assertContains(t, serverDeployment, `image: "gophprofile-server:local"`)
	assertContains(t, serverDeployment, "imagePullPolicy: Never")
	assertContains(t, workerDeployment, `image: "gophprofile-worker:local"`)
	assertContains(t, workerDeployment, "imagePullPolicy: Never")
	assertContains(t, migrateJob, `image: "gophprofile-migrate:local"`)
	assertContains(t, migrateJob, "imagePullPolicy: Never")
}

func renderChart(chartPath string, args ...string) ([]byte, error) {
	commandArgs := []string{"template", "gophprofile", chartPath, "--namespace", "gophprofile"}
	commandArgs = append(commandArgs, args...)

	return exec.Command("helm", commandArgs...).CombinedOutput()
}

func findDocument(t *testing.T, rendered, kind, name string) string {
	t.Helper()

	for _, document := range strings.Split(rendered, "\n---\n") {
		if strings.Contains(document, "kind: "+kind+"\n") &&
			strings.Contains(document, "\n  name: "+name+"\n") {
			return document
		}
	}

	t.Fatalf("rendered %s %q not found", kind, name)
	return ""
}

func findFieldValue(t *testing.T, document, field string) string {
	t.Helper()

	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, field) {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, field))
			if value == "" {
				t.Fatalf("rendered field %q has empty value", field)
			}
			return value
		}
	}

	t.Fatalf("rendered document does not contain field %q", field)
	return ""
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("rendered document does not contain %q", want)
	}
}

func assertNotContains(t *testing.T, value, unwanted string) {
	t.Helper()
	if strings.Contains(value, unwanted) {
		t.Fatalf("rendered document unexpectedly contains %q", unwanted)
	}
}
