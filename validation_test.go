package ohm

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestValidateRunsBuiltInCustomAndNestedRules(t *testing.T) {
	form := validationPostForm{
		Title:       " ",
		Body:        "This body is too long",
		Count:       0,
		PublishedAt: time.Time{},
		Tags:        []string{},
		Accepted:    false,
		Start:       time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 6, 7, 11, 0, 0, 0, time.UTC),
		Author:      validationAuthor{Name: ""},
	}

	errs := Validate(form)

	if !errs.Any() {
		t.Fatalf("Validate(%#v).Any() = false, want true", form)
	}
	if errs.Len() != 8 {
		t.Fatalf("Validate(%#v).Len() = %d, want %d", form, errs.Len(), 8)
	}

	requireValidationError(t, errs, "title", ValidationCodeRequired)
	requireValidationError(t, errs, "body", ValidationCodeTooLong)
	requireValidationError(t, errs, "count", ValidationCodeTooSmall)
	requireValidationError(t, errs, "published_at", ValidationCodeRequired)
	requireValidationError(t, errs, "tags", ValidationCodeTooShort)
	requireValidationError(t, errs, "accepted", ValidationCodeAccepted)
	requireValidationError(t, errs, "end", "after_start")
	requireValidationError(t, errs, "author.name", ValidationCodeRequired)
}

func TestValidateReturnsEmptyErrorsForValidValue(t *testing.T) {
	form := validationPostForm{
		Title:       "Hello",
		Body:        "Short body",
		Count:       2,
		PublishedAt: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		Tags:        []string{"go"},
		Accepted:    true,
		Start:       time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC),
		Author:      validationAuthor{Name: "Ada"},
	}

	errs := Validate(form)

	if errs.Any() {
		t.Fatalf("Validate(%#v).Any() = true, want false with errors %#v", form, errs.All())
	}
	if errs.Len() != 0 {
		t.Errorf("Validate(%#v).Len() = %d, want 0", form, errs.Len())
	}
	if all := errs.All(); len(all) != 0 {
		t.Errorf("Validate(%#v).All() = %#v, want empty", form, all)
	}
}

func TestValidationStringRules(t *testing.T) {
	validation := NewValidation()
	validation.String("blank", "  ").Presence().Length(3, 5)
	validation.String("short", "Go").Length(3, 5)
	validation.String("long", "Gophers").Length(3, 5)
	validation.String("unicode", "åß").Min(3)
	validation.String("optional_blank", "           ").Max(3)

	errs := validation.Errors()

	if errs.Len() != 4 {
		t.Fatalf("validation.Errors().Len() = %d, want %d with errors %#v", errs.Len(), 4, errs.All())
	}
	requireValidationError(t, errs, "blank", ValidationCodeRequired)
	requireValidationError(t, errs, "short", ValidationCodeTooShort)
	requireValidationError(t, errs, "long", ValidationCodeTooLong)
	requireValidationError(t, errs, "unicode", ValidationCodeTooShort)
	if errs.For("blank").Len() != 1 {
		t.Errorf("validation.Errors().For(%q).Len() = %d, want 1", "blank", errs.For("blank").Len())
	}
}

func TestValidationIntBoolTimeAndSliceRules(t *testing.T) {
	validation := NewValidation()
	validation.Int("small", 2).Min(3)
	validation.Int("large", 7).Max(5)
	validation.Int("below_between", 2).Between(3, 5)
	validation.Int("outside", 10).Between(3, 5)
	validation.Bool("accepted", false).Accepted()
	validation.Bool("rejected", true).Rejected()
	afterBoundary := time.Date(2026, 6, 7, 13, 0, 0, 123, time.UTC)
	beforeBoundary := time.Date(2026, 6, 7, 11, 0, 0, 456, time.UTC)
	validation.Time("published_at", time.Time{}).Presence()
	validation.Time("starts_at", time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)).
		After(afterBoundary)
	validation.Time("ends_at", time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)).
		Before(beforeBoundary)
	var nilTags []string
	validation.Slice("empty", []string{}).Presence()
	validation.Slice("nil_tags", nilTags).Presence()
	validation.Slice("few", []int{1}).Min(2)
	validation.Slice("many", [3]int{1, 2, 3}).Max(2)
	validation.Slice("invalid", "not a slice").Min(1)

	errs := validation.Errors()

	if errs.Len() != 14 {
		t.Fatalf("validation.Errors().Len() = %d, want %d with errors %#v", errs.Len(), 14, errs.All())
	}
	small := requireValidationError(t, errs, "small", ValidationCodeTooSmall)
	if value, ok := small.Param("min"); !ok || value != "3" {
		t.Errorf("validation.Errors().First(%q).Param(%q) = %q, %t, want %q, true", "small", "min", value, ok, "3")
	}
	large := requireValidationError(t, errs, "large", ValidationCodeTooLarge)
	if value, ok := large.Param("max"); !ok || value != "5" {
		t.Errorf("validation.Errors().First(%q).Param(%q) = %q, %t, want %q, true", "large", "max", value, ok, "5")
	}
	requireValidationError(t, errs, "below_between", ValidationCodeTooSmall)
	requireValidationError(t, errs, "outside", ValidationCodeTooLarge)
	requireValidationError(t, errs, "accepted", ValidationCodeAccepted)
	requireValidationError(t, errs, "rejected", ValidationCodeRejected)
	requireValidationError(t, errs, "published_at", ValidationCodeRequired)
	startsAt := requireValidationError(t, errs, "starts_at", ValidationCodeTooSmall)
	if value, ok := startsAt.Param("after"); !ok || value != "2026-06-07T13:00:00.000000123Z" {
		t.Errorf("validation.Errors().First(%q).Param(%q) = %q, %t, want %q, true", "starts_at", "after", value, ok, "2026-06-07T13:00:00.000000123Z")
	}
	endsAt := requireValidationError(t, errs, "ends_at", ValidationCodeTooLarge)
	if value, ok := endsAt.Param("before"); !ok || value != "2026-06-07T11:00:00.000000456Z" {
		t.Errorf("validation.Errors().First(%q).Param(%q) = %q, %t, want %q, true", "ends_at", "before", value, ok, "2026-06-07T11:00:00.000000456Z")
	}
	requireValidationError(t, errs, "empty", ValidationCodeTooShort)
	requireValidationError(t, errs, "nil_tags", ValidationCodeTooShort)
	requireValidationError(t, errs, "few", ValidationCodeTooShort)
	requireValidationError(t, errs, "many", ValidationCodeTooLong)
	requireValidationError(t, errs, "invalid", ValidationCodeInvalid)
}

func TestValidationNestedSkipsNilValues(t *testing.T) {
	var author *validationAuthor
	validation := NewValidation()

	validation.Nested("author", author)

	if errs := validation.Errors(); errs.Any() {
		t.Errorf("validation.Nested(%q, nil).Errors() = %#v, want empty", "author", errs.All())
	}
}

func TestValidateSkipsNilValidatable(t *testing.T) {
	var form *validationPointerForm

	errs := Validate(form)

	if errs.Any() {
		t.Errorf("Validate(nil validatable).Any() = true, want false with errors %#v", errs.All())
	}
}

func TestValidationErrorsCanBeInspectedAndCloned(t *testing.T) {
	validation := NewValidation()
	validation.String("title", "Go").Min(3)
	validation.Add("title", "custom", "custom message", ErrorParam{Name: "rule", Value: "custom"})

	errs := validation.Errors()

	if !errors.Is(errs, ErrValidation) {
		t.Errorf("errors.Is(validation.Errors(), ErrValidation) = false, want true")
	}
	if err := errs.Err(); !errors.Is(err, ErrValidation) {
		t.Errorf("validation.Errors().Err() = %v, want ErrValidation", err)
	}
	if err := (Errors{}).Err(); err != nil {
		t.Errorf("empty validation Errors.Err() = %v, want nil", err)
	}
	if errors.Is(Errors{}, ErrValidation) {
		t.Errorf("errors.Is(empty validation Errors, ErrValidation) = true, want false")
	}
	if got := errs.Error(); got != "validation failed" {
		t.Errorf("validation.Errors().Error() = %q, want %q", got, "validation failed")
	}
	if messages := errs.Messages("title"); !slices.Equal(messages, []string{"must be at least 3 characters", "custom message"}) {
		t.Errorf("validation.Errors().Messages(%q) = %#v, want %#v", "title", messages, []string{"must be at least 3 characters", "custom message"})
	}

	first, ok := errs.First("title")
	if !ok {
		t.Fatalf("validation.Errors().First(%q) ok = false, want true", "title")
	}
	if first.Code != ValidationCodeTooShort {
		t.Errorf("validation.Errors().First(%q).Code = %q, want %q", "title", first.Code, ValidationCodeTooShort)
	}
	if value, ok := first.Param("min"); !ok || value != "3" {
		t.Errorf("validation.Errors().First(%q).Param(%q) = %q, %t, want %q, true", "title", "min", value, ok, "3")
	}

	filtered := errs.For("title")
	if filtered.Len() != 2 {
		t.Fatalf("validation.Errors().For(%q).Len() = %d, want 2", "title", filtered.Len())
	}
	filtered[0].Params["min"] = "changed"

	again, ok := validation.Errors().First("title")
	if !ok {
		t.Fatalf("validation.Errors().First(%q) after mutation ok = false, want true", "title")
	}
	if value, _ := again.Param("min"); value != "3" {
		t.Errorf("validation.Errors().First(%q).Param(%q) after mutation = %q, want %q", "title", "min", value, "3")
	}

	all := errs.All()
	all[0].Params["min"] = "changed again"
	if value, _ := errs[0].Param("min"); value != "3" {
		t.Errorf("validation.Errors()[0].Param(%q) after All mutation = %q, want %q", "min", value, "3")
	}
}

func TestValidationErrorsMarshalAsStructuredJSON(t *testing.T) {
	validation := NewValidation()
	validation.String("title", "Go").Min(3)

	body, err := json.Marshal(validation.Errors())
	if err != nil {
		t.Fatalf("json.Marshal(validation.Errors()) error = %v, want nil", err)
	}

	want := `[{"field":"title","code":"too_short","message":"must be at least 3 characters","params":{"min":"3"}}]`
	if string(body) != want {
		t.Errorf("json.Marshal(validation.Errors()) = %s, want %s", body, want)
	}
}

func TestValidationAddUsesDefaults(t *testing.T) {
	validation := NewValidation()

	validation.Add("", "", "")

	err, ok := validation.Errors().First("")
	if !ok {
		t.Fatalf("validation.Add(empty defaults).Errors().First(empty) ok = false, want true")
	}
	if err.Code != ValidationCodeInvalid {
		t.Errorf("validation.Add(empty defaults).Code = %q, want %q", err.Code, ValidationCodeInvalid)
	}
	if err.Message != ValidationCodeInvalid {
		t.Errorf("validation.Add(empty defaults).Message = %q, want %q", err.Message, ValidationCodeInvalid)
	}
}

func requireValidationError(t *testing.T, errs Errors, field string, code string) ValidationError {
	t.Helper()

	for _, err := range errs {
		if err.Field == field && err.Code == code {
			return err
		}
	}
	t.Fatalf("validation errors = %#v, want field %q with code %q", errs.All(), field, code)
	return ValidationError{}
}

type validationPostForm struct {
	Title       string
	Body        string
	Count       int
	PublishedAt time.Time
	Tags        []string
	Accepted    bool
	Start       time.Time
	End         time.Time
	Author      validationAuthor
}

func (f validationPostForm) Validate(v *Validation) {
	v.String("title", f.Title).Presence().Length(3, 120)
	v.String("body", f.Body).Max(10)
	v.Int("count", f.Count).Min(1)
	v.Time("published_at", f.PublishedAt).Presence()
	v.Slice("tags", f.Tags).Min(1)
	v.Bool("accepted", f.Accepted).Accepted()
	if f.End.Before(f.Start) {
		v.Add("end", "after_start", "must be after the start time")
	}
	v.Nested("author", f.Author)
}

type validationAuthor struct {
	Name string
}

func (a validationAuthor) Validate(v *Validation) {
	v.String("name", a.Name).Presence()
}

type validationPointerForm struct{}

func (*validationPointerForm) Validate(v *Validation) {
	v.Add("value", "unexpected", "should not run")
}

type exampleSignupForm struct {
	Email string
	Age   int
}

func (f exampleSignupForm) Validate(v *Validation) {
	v.String("email", f.Email).Presence()
	v.Int("age", f.Age).Min(13)
}

func ExampleValidate() {
	form := exampleSignupForm{
		Email: "",
		Age:   12,
	}

	errs := Validate(form)
	for _, err := range errs.All() {
		fmt.Println(err.Field, err.Code, err.Message)
	}

	// Output:
	// email required is required
	// age too_small must be at least 13
}
