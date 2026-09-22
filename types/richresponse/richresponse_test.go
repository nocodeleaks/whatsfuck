// Copyright (c) 2026 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package richresponse

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRichResponseDecodesNestedContent(t *testing.T) {
	var response RichResponse
	err := json.Unmarshal([]byte(`{
		"response_id":"response-1",
		"sections":[{
			"__typename":"GenAIGridLayoutViewModel",
			"primitives":[
				{"__typename":"FOATextPrimitive","text":"Hello {{link}}world{{/link}}"},
				{"__typename":"GenAIMarkdownTextUXPrimitive","text":"{{link}}source{{/link}}","inline_entities":[{
					"key":"link","metadata":{"__typename":"GenAIInlineLinkItem","url":"https://example.com","display_name":"source"}
				}]},
				{"__typename":"GenAICodeUXPrimitive","language":"go","code_blocks":[{"content":"hello","type":"STR"},{"content":"()","type":"DEFAULT"}]}
			]
		}],
		"footer_sections":[{"__typename":"GenAISingleLayoutViewModel","primitive":{"__typename":"GenAIMetadataTextPrimitive","text":"footer"}}]
	}`), &response)
	if err != nil {
		t.Fatal(err)
	}
	if response.ResponseID != "response-1" || len(response.Sections) != 1 || len(response.FooterSections) != 1 {
		t.Fatalf("unexpected envelope: %#v", response)
	}
	if got := response.Sections[0].Model.String(); got != "Hello world\nsource\nhello()" {
		t.Fatalf("unexpected rendered content: %q", got)
	}
	primitives := response.Sections[0].Model.GetPrimitives()
	markdown, ok := primitives[1].(*GenAIMarkdownTextUXPrimitive)
	if !ok || len(markdown.InlineEntities) != 1 {
		t.Fatalf("missing inline entity: %#v", primitives[1])
	}
	want := &GenAIInlineLinkItem{URL: "https://example.com", DisplayName: "source"}
	if entity := markdown.InlineEntities[0]; entity.Key != "link" || !reflect.DeepEqual(entity.Metadata, want) {
		t.Fatalf("unexpected inline entity: %#v", entity)
	}
	if got := response.FooterSections[0].Model.String(); got != "footer" {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestContainersRejectMalformedContent(t *testing.T) {
	for _, input := range []string{
		`{"__typename":"FOATextPrimitive","text":42}`,
		`{"__typename":"GenAICodeUXPrimitive","code_blocks":"invalid"}`,
		`{"__typename":42}`,
		`[]`,
		`"text"`,
		`{"__typename":`,
	} {
		t.Run("primitive/"+input, func(t *testing.T) {
			original := &FOATextPrimitive{Text: "original"}
			got := PrimitiveContainer{Value: original}
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("malformed primitive accepted")
			}
			if got.Value != original {
				t.Fatal("failed decoding replaced the previous primitive")
			}
		})
	}
	for _, input := range []string{
		`{"__typename":"GenAIGridLayoutViewModel","primitives":42}`,
		`{"__typename":"GenAISingleLayoutViewModel","primitive":{"__typename":"FOATextPrimitive","text":42}}`,
		`{"__typename":"GenAIGridLayoutViewModel","primitives":[{"__typename":"GenAIMarkdownTextUXPrimitive","inline_entities":[{"metadata":{"__typename":"GenAIInlineLinkItem","url":42}}]}]}`,
		`{"__typename":42}`,
		`[]`,
	} {
		t.Run("viewmodel/"+input, func(t *testing.T) {
			original := &GenAISingleLayoutViewModel{}
			got := ViewModelContainer{Model: original}
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("malformed view model accepted")
			}
			if got.Model != original {
				t.Fatal("failed decoding replaced the previous view model")
			}
		})
	}
}

func TestPrimitiveContainerReplacesValueAndPreservesUnknown(t *testing.T) {
	got := PrimitiveContainer{Value: &FOATextPrimitive{Text: "old"}}
	if err := json.Unmarshal([]byte(`{"__typename":"GenAIMetadataTextPrimitive","text":"new"}`), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Value, &GenAIMetadataTextPrimitive{Text: "new"}) {
		t.Fatalf("primitive was not replaced: %#v", got.Value)
	}
	for _, input := range []string{
		`{"__typename":"FuturePrimitive", "payload":{"enabled":true}}`,
		`{}`,
		`null`,
	} {
		data := []byte(input)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		data[0] = '!'
		raw, ok := got.Value.(UnknownPrimitive)
		if !ok || string(raw) != input {
			t.Fatalf("unknown primitive data was not copied: %#v", got.Value)
		}
		if got.Value.String() != "" {
			t.Fatal("unknown primitive should render as empty text")
		}
	}
}

func TestViewModelContainerReplacesModelAndPreservesUnknown(t *testing.T) {
	got := ViewModelContainer{Model: &GenAISingleLayoutViewModel{}}
	for _, typeName := range []string{"GenAIGridLayoutViewModel", "GenAIHScrollLayoutViewModel", "GenAIVStackLayoutViewModel"} {
		input := `{"__typename":"` + typeName + `","primitives":[{"__typename":"FOATextPrimitive","text":"new"}]}`
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatal(err)
		}
		if _, ok := got.Model.(*MultiLayoutViewModel); !ok || got.Model.String() != "new" {
			t.Fatalf("view model was not replaced: %#v", got.Model)
		}
	}
	for _, input := range []string{
		`{"__typename":"FutureViewModel", "payload":{"enabled":true}}`,
		`{}`,
		`null`,
	} {
		data := []byte(input)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		data[0] = '!'
		raw, ok := got.Model.(UnknownViewModel)
		if !ok || string(raw) != input {
			t.Fatalf("unknown view model data was not copied: %#v", got.Model)
		}
		if got.Model.String() != "" || len(got.Model.GetPrimitives()) != 0 {
			t.Fatal("unknown view model should render as empty text without primitives")
		}
	}
}

func TestEmptyAndNullContentRendersSafely(t *testing.T) {
	for _, input := range []string{
		`{"__typename":"GenAISingleLayoutViewModel"}`,
		`{"__typename":"GenAISingleLayoutViewModel","primitive":null}`,
		`{"__typename":"GenAIGridLayoutViewModel"}`,
		`{"__typename":"GenAIGridLayoutViewModel","primitives":null}`,
		`{"__typename":"GenAIGridLayoutViewModel","primitives":[null]}`,
	} {
		t.Run(input, func(t *testing.T) {
			var got ViewModelContainer
			if err := json.Unmarshal([]byte(input), &got); err != nil {
				t.Fatal(err)
			}
			if text := got.Model.String(); text != "" {
				t.Fatalf("expected empty text, got %q", text)
			}
		})
	}
	var table PrimitiveContainer
	if err := json.Unmarshal([]byte(`{"__typename":"GenATableUXPrimitive","rows":[null,{"cells":["a","b"]}]}`), &table); err != nil {
		t.Fatal(err)
	}
	if got := table.Value.String(); got != "\na | b" {
		t.Fatalf("unexpected table rendering: %q", got)
	}
	model := MultiLayoutViewModel{Primitives: []PrimitiveContainer{{}}}
	if model.String() != "" {
		t.Fatal("a zero-valued primitive should render as empty text")
	}
}

func TestTextEntityDecodesAndReplacesMetadata(t *testing.T) {
	var got TextEntity
	for _, tc := range []struct {
		input string
		want  TextEntityMetadata
	}{
		{`{"__typename":"GenAIInlineLinkItem","url":"https://example.com","display_name":"example"}`, &GenAIInlineLinkItem{URL: "https://example.com", DisplayName: "example"}},
		{`{"__typename":"GenAIDeepLinkItem","deeplink_url":"app://item","text":"item"}`, &GenAIDeepLinkItem{DeepLinkURL: "app://item", Text: "item"}},
		{`{"__typename":"GenAILatexItem","latex_expression":"x^2"}`, &GenAILatexItem{LatexExpression: "x^2"}},
		{`{"__typename":"GenAISearchCitationItem"}`, &GenAISearchCitationItem{}},
		{`{"__typename":"FutureMetadata", "payload":[1,2]}`, UnknownTextEntityMetadata(`{"__typename":"FutureMetadata", "payload":[1,2]}`)},
		{`{}`, UnknownTextEntityMetadata(`{}`)},
		{`null`, UnknownTextEntityMetadata(`null`)},
	} {
		t.Run(tc.input, func(t *testing.T) {
			data := []byte(`{"key":"new","metadata":` + tc.input + `}`)
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			for index := range data {
				data[index] = '!'
			}
			if got.Key != "new" || !reflect.DeepEqual(got.Metadata, tc.want) {
				t.Fatalf("unexpected decoded entity: %#v; metadata: %#v", got, got.Metadata)
			}
		})
	}
	for _, input := range []string{`{"key":""}`, `{}`, `null`} {
		got = TextEntity{Key: "old", Metadata: &GenAIInlineLinkItem{URL: "old"}}
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatal(err)
		}
		if got.Key != "" || got.Metadata != nil {
			t.Fatalf("absent metadata did not clear reused entity: %#v", got)
		}
	}
}

func TestTextEntityRejectsMalformedMetadata(t *testing.T) {
	for _, input := range []string{
		`{"key":42}`,
		`{"metadata":{"__typename":42}}`,
		`{"metadata":{"__typename":"GenAIInlineLinkItem","url":42}}`,
		`{"metadata":42}`,
		`{"metadata":[]}`,
		`{"metadata":"invalid"}`,
		`{"metadata":`,
		`[]`,
	} {
		t.Run(input, func(t *testing.T) {
			original := TextEntity{Key: "original", Metadata: &GenAIInlineLinkItem{URL: "original"}}
			got := original
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("malformed text entity accepted")
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("failed decoding changed the previous entity: %#v", got)
			}
		})
	}
}

func TestRichResponseRejectsMalformedNestedPrimitive(t *testing.T) {
	var response RichResponse
	err := json.Unmarshal([]byte(`{"sections":[{"__typename":"GenAISingleLayoutViewModel","primitive":{"__typename":"FOATextPrimitive","text":42}}]}`), &response)
	if err == nil || !strings.Contains(err.Error(), "text") {
		t.Fatalf("expected the nested text field error, got %v", err)
	}
}
