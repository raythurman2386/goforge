package services

import (
	"math"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const mib = 1024 * 1024

func TestParseCPU(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "  ", want: 0},
		{in: "0", want: 0},
		{in: "0.25", want: 250_000_000},
		{in: "0.5", want: 500_000_000},
		{in: " 0.5 ", want: 500_000_000},
		{in: "1", want: 1_000_000_000},
		{in: "1.0", want: 1_000_000_000},
		{in: "2.5", want: 2_500_000_000},
		{in: "0.000000001", want: 1},
		{in: "0.0000000001", wantErr: true}, // finer than one NanoCPU
		{in: "-1", wantErr: true},
		{in: "half", wantErr: true},
		{in: "500m", wantErr: true}, // Kubernetes millicores are not Docker --cpus
		{in: "1e30", wantErr: true},
		{in: "99999999999", wantErr: true}, // overflows int64 NanoCPUs
		{in: "9223372036.854775807", want: math.MaxInt64},
		{in: "9223372036.854775808", wantErr: true},
		{in: "9223372036.999999999", wantErr: true},
		{in: ".5", want: 500_000_000},
		{in: "0.2500000000", want: 250_000_000}, // trailing zeros are not extra precision
		{in: ".", wantErr: true},
		{in: "1.2.3", wantErr: true},
		{in: "+1", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseCPU(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCPU(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseCPU(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseMemory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "128m", want: 128 * mib},
		{in: "256m", want: 256 * mib},
		{in: "256M", want: 256 * mib},
		{in: "512MB", want: 512 * mib},
		{in: "512mb", want: 512 * mib},
		{in: "512MiB", want: 512 * mib},
		{in: " 512m ", want: 512 * mib},
		{in: "1g", want: 1024 * mib},
		{in: "1G", want: 1024 * mib},
		{in: "1GB", want: 1024 * mib},
		{in: "1.5g", want: 1536 * mib},
		{in: "64k", want: 64 * 1024},
		{in: "1048576", want: mib}, // plain bytes, as with docker run --memory
		{in: "lots", wantErr: true},
		{in: "-256m", wantErr: true},
		{in: "256x", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseMemory(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseMemory(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMemory(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// Every shipped template must have resources that parse, or deploys of it fail.
func TestRepoTemplates_ResourcesParse(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)
	for _, tmpl := range c.ListTemplates() {
		cpu, mem, err := tmpl.Resources.Limits()
		if err != nil {
			t.Errorf("template %s: %v", tmpl.Name, err)
			continue
		}
		if cpu == 0 || mem == 0 {
			t.Errorf("template %s: cpu=%d memory=%d, want both set", tmpl.Name, cpu, mem)
		}
	}
}

// Issue #37: template resources must reach the Docker container options.
func TestDeployInstance_AppliesTemplateResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tmpl       string
		config     map[string]interface{}
		wantCPU    int64
		wantMemory int64
	}{
		{
			tmpl:       "vaultwarden",
			config:     map[string]interface{}{"name": "vault", "domain": "vault.example.com", "signups_allowed": "false"},
			wantCPU:    250_000_000, // cpu: "0.25"
			wantMemory: 256 * mib,   // memory: "256m"
		},
		{
			tmpl:       "solidcraft",
			config:     map[string]interface{}{"name": "cad", "domain": "cad.example.com"},
			wantCPU:    250_000_000, // cpu: "0.25"
			wantMemory: 128 * mib,   // memory: "128m"
		},
	}
	for _, tt := range tests {
		t.Run(tt.tmpl, func(t *testing.T) {
			t.Parallel()
			opts := deployRepoTemplate(t, tt.tmpl, true, tt.config)
			if opts.NanoCPUs != tt.wantCPU {
				t.Errorf("NanoCPUs = %d, want %d", opts.NanoCPUs, tt.wantCPU)
			}
			if opts.Memory != tt.wantMemory {
				t.Errorf("Memory = %d, want %d", opts.Memory, tt.wantMemory)
			}
		})
	}
}

func TestBuildContainerSpec_Resources(t *testing.T) {
	t.Parallel()
	svc := &InstanceService{}
	inst := &models.ServiceInstance{ID: "inst-1"}

	unset := &ServiceTemplate{Name: "plain", Docker: DockerConfig{Image: "nginx"}}
	spec, err := svc.buildContainerSpec(unset, inst, nil)
	if err != nil {
		t.Fatalf("buildContainerSpec(no resources) error = %v", err)
	}
	if spec.opts.NanoCPUs != 0 || spec.opts.Memory != 0 {
		t.Errorf("no resources: NanoCPUs=%d Memory=%d, want 0 (unlimited)", spec.opts.NanoCPUs, spec.opts.Memory)
	}

	bad := &ServiceTemplate{Name: "bad", Resources: ResourceConfig{CPU: "0.5", Memory: "lots"}, Docker: DockerConfig{Image: "nginx"}}
	if _, err := svc.buildContainerSpec(bad, inst, nil); err == nil {
		t.Error("buildContainerSpec(invalid memory) error = nil, want error")
	}
}
