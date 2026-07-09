// Package e2e provides mock-based end-to-end tests for the wau-go-sdk 5-platform bot SDKs.
//
// Strategy (per W7.2 closure, 2026-07-09):
//   - For each platform, stand up an in-process HTTP server (httptest.NewServer or net.Listen for SMTP).
//   - Use reflection + unsafe to inject mock clients into the bot's unexported fields
//     (e.g. *slack.Client, *lark.Client, openapi.OpenAPI, webhook cache map).
//     This avoids any modification of production code under bot/{slack,feishu,qq,dingtalk,email}/*.
//   - Standard library testing only (no testify).
//
// All tests in this package are zero-credential: they run with `go test -count=1 ./tests/e2e/...`
// without any environment variables or external services.
package e2e

import (
	"reflect"
	"testing"
	"unsafe"
)

// setPrivateField uses reflection + unsafe.Pointer to set an unexported field on a struct.
//
// Required because the 5 bot SDKs keep their native SDK clients as unexported fields
// (e.g. botslack.SlackBot.api), and our test files live in a separate package
// (package e2e) so we cannot use direct field assignment.
//
// Behavior:
//   - obj must be a non-nil pointer to a struct.
//   - fieldName must match an unexported field name on the struct.
//   - value must be assignable to that field's type (or convertible via reflection).
//
// On failure the test is fatally failed with a clear message.
func setPrivateField(t *testing.T, obj interface{}, fieldName string, value interface{}) {
	t.Helper()
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		t.Fatalf("setPrivateField: obj must be a non-nil pointer to a struct, got %T", obj)
	}
	v = v.Elem()
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("setPrivateField: field %q not found on %T", fieldName, obj)
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		t.Fatalf("setPrivateField: value for field %q is invalid", fieldName)
	}
	// Go 1.22+: AssignableTo lives on Type, not Value.
	if !field.Type().AssignableTo(rv.Type()) && !rv.Type().AssignableTo(field.Type()) {
		if rv.Type().ConvertibleTo(field.Type()) {
			rv = rv.Convert(field.Type())
		} else {
			t.Fatalf("setPrivateField: field %q (%s) not assignable from %s", fieldName, field.Type(), rv.Type())
		}
	}
	// Field is unexported; use unsafe.Pointer to bypass the export restriction.
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(rv)
}

// getPrivateField uses reflection + unsafe.Pointer to read an unexported field.
func getPrivateField(t *testing.T, obj interface{}, fieldName string) reflect.Value {
	t.Helper()
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		t.Fatalf("getPrivateField: obj must be a non-nil pointer to a struct, got %T", obj)
	}
	v = v.Elem()
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("getPrivateField: field %q not found on %T", fieldName, obj)
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}
