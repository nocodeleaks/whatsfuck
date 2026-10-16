package whatsmeow

import (
	"testing"

	waBinary "github.com/nocodeleaks/whatsfuck/binary"
	"github.com/nocodeleaks/whatsfuck/proto/waE2E"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestInteractiveLinkedDeviceBusinessMetadata(t *testing.T) {
	for _, test := range []struct{ name, raw, tag, flow string }{
		{"reply", `{"interactiveMessage":{"nativeFlowMessage":{"buttons":[{"name":"quick_reply"}]}}}`, "interactive", "mixed"},
		{"url", `{"interactiveMessage":{"nativeFlowMessage":{"buttons":[{"name":"cta_url"}]}}}`, "interactive", "mixed"},
		{"three CTA", `{"interactiveMessage":{"nativeFlowMessage":{"buttons":[{"name":"cta_url"},{"name":"cta_call"},{"name":"cta_copy"}]}}}`, "interactive", "mixed"},
		{"form", `{"interactiveMessage":{"nativeFlowMessage":{"buttons":[{"name":"form_message"}]}}}`, "interactive", "form_message"},
		{"catalog", `{"interactiveMessage":{"nativeFlowMessage":{"buttons":[{"name":"catalog_message"}]}}}`, "interactive", "catalog_message"},
		{"carousel", `{"interactiveMessage":{"carouselMessage":{"cards":[{"nativeFlowMessage":{"buttons":[{"name":"cta_url"}]}}]}}}`, "interactive", "mixed"},
		{"wrapped list", `{"documentWithCaptionMessage":{"message":{"listMessage":{"listType":1}}}}`, "list", ""},
		{"wrapped buttons", `{"documentWithCaptionMessage":{"message":{"buttonsMessage":{}}}}`, "interactive", "mixed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var message waE2E.Message
			if err := protojson.Unmarshal([]byte(test.raw), &message); err != nil {
				t.Fatal(err)
			}
			content := (&Client{}).getMessageContent(waBinary.Node{Tag: "enc"}, &message, waBinary.Attrs{}, false, nodeExtraParams{})
			if len(content) != 2 || content[1].Tag != "biz" || len(content[1].Attrs) != 0 {
				t.Fatalf("unexpected companion: %v", content)
			}
			children := content[1].Content.([]waBinary.Node)
			if len(children) != 1 || children[0].Tag != test.tag {
				t.Fatalf("unexpected business metadata: %v", children)
			}
			if test.flow != "" {
				flow := children[0].Content.([]waBinary.Node)[0]
				if flow.Attrs["name"] != test.flow {
					t.Fatalf("wrong flow: %v", flow)
				}
				if test.flow == "mixed" && flow.Attrs["v"] != "9" {
					t.Fatal("mixed flow version missing")
				}
			} else if test.tag == "list" && (children[0].Attrs["type"] != "product_list" || children[0].Attrs["v"] != "2") {
				t.Fatal("list attrs lost")
			}
			if test.name == "wrapped buttons" && (getTypeFromMessage(&message) != "media" || getMediaTypeFromMessage(&message) != "button") {
				t.Fatal("legacy buttons must use media/button stanza attributes")
			}
			if test.name == "wrapped list" && (getTypeFromMessage(&message) != "media" || getMediaTypeFromMessage(&message) != "list" || message.GetDocumentWithCaptionMessage().GetMessage().GetListMessage().GetListType() != waE2E.ListMessage_SINGLE_SELECT) {
				t.Fatal("wrapped list must keep the single-select payload with media/list stanza attributes")
			}
		})
	}
}
