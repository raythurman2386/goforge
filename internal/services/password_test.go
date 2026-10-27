package services

import "testing"

func TestFillGeneratedConfig(t *testing.T) {
	t.Parallel()

	tmpl := &ServiceTemplate{Config: []ConfigField{
		{Name: "password", Generate: true, Length: 8},
		{Name: "token", Generate: true, Length: 32},
		{Name: "username", Generate: false},
	}}

	blank := map[string]interface{}{"password": "", "username": "appuser"}
	if err := fillGeneratedConfig(tmpl, blank); err != nil {
		t.Fatal(err)
	}
	password, _ := blank["password"].(string)
	if len(password) != defaultPasswordSize {
		t.Fatalf("short generate length = %d, want %d", len(password), defaultPasswordSize)
	}
	token, _ := blank["token"].(string)
	if len(token) != 32 {
		t.Fatalf("token length = %d, want 32", len(token))
	}
	if blank["username"] != "appuser" {
		t.Fatalf("username changed: %v", blank["username"])
	}

	kept := map[string]interface{}{"password": "keep-me"}
	if err := fillGeneratedConfig(tmpl, kept); err != nil {
		t.Fatal(err)
	}
	if kept["password"] != "keep-me" {
		t.Fatalf("existing password replaced: %v", kept["password"])
	}
}
