// Copyright (c) 2026 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package richresponse

import (
	"encoding/json"
	"reflect"
)

type TextEntity struct {
	Key      string             `json:"key"`
	Metadata TextEntityMetadata `json:"metadata"`
}

func (te *TextEntity) UnmarshalJSON(data []byte) error {
	// Decode the envelope separately: embedding TextEntity would promote this
	// method to the envelope and recursively invoke it again.
	var raw struct {
		Key      string          `json:"key"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	decoded := TextEntity{Key: raw.Key}
	if len(raw.Metadata) > 0 {
		val, err := unmarshalWithTypeName[UnknownTextEntityMetadata](raw.Metadata, textEntityMetadataTypes)
		if err != nil {
			return err
		}
		decoded.Metadata = val.(TextEntityMetadata)
	}
	*te = decoded
	return nil
}

type TextEntityMetadata interface {
	isTextEntityMetadata()
}

var textEntityMetadataTypes = map[string]reflect.Type{
	"GenAISearchCitationItem": reflect.TypeFor[GenAISearchCitationItem](),
	"GenAIInlineLinkItem":     reflect.TypeFor[GenAIInlineLinkItem](),
	"GenAIDeepLinkItem":       reflect.TypeFor[GenAIDeepLinkItem](),
	"GenAILatexItem":          reflect.TypeFor[GenAILatexItem](),
}

func (*GenAISearchCitationItem) isTextEntityMetadata()  {}
func (*GenAIInlineLinkItem) isTextEntityMetadata()      {}
func (*GenAIDeepLinkItem) isTextEntityMetadata()        {}
func (*GenAILatexItem) isTextEntityMetadata()           {}
func (UnknownTextEntityMetadata) isTextEntityMetadata() {}

type GenAISearchCitationItem struct {
}

type GenAIInlineLinkItem struct {
	URL         string `json:"url"`
	DisplayName string `json:"display_name"`
}

type GenAIDeepLinkItem struct {
	DeepLinkURL string `json:"deeplink_url"`
	Text        string `json:"text"`
}

type GenAILatexItem struct {
	LatexExpression string `json:"latex_expression"`
}

type UnknownTextEntityMetadata json.RawMessage
