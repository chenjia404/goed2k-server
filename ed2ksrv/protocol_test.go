package ed2ksrv

import (
	"bytes"
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestParseSearchRequestRecursiveTree(t *testing.T) {
	var body bytes.Buffer
	writeSearchBool(&body, searchOpEqual)
	writeSearchStringTerm(&body, "ubuntu")
	writeSearchStringTerm(&body, "iso")

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse recursive search: %v", err)
	}
	if len(parsed.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %#v", parsed.Keywords)
	}
}

func TestParseSearchRequestORKeywords(t *testing.T) {
	var body bytes.Buffer
	writeSearchBool(&body, searchOpGreater)
	writeSearchStringTerm(&body, "mkv")
	writeSearchStringTerm(&body, "avi")

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse OR search: %v", err)
	}
	if len(parsed.KeywordAlternatives) != 2 {
		t.Fatalf("expected OR alternatives, got %#v", parsed)
	}
}

func TestParseSearchRequestFilenameTag(t *testing.T) {
	var body bytes.Buffer
	if err := body.WriteByte(searchTypeStrTag); err != nil {
		t.Fatal(err)
	}
	writeSearchStringValue(&body, "demo.mp3")
	_ = protocol.WriteUInt16(&body, 1)
	_ = body.WriteByte(protocol.FTFilename)

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse filename tag: %v", err)
	}
	if len(parsed.Keywords) != 1 || parsed.Keywords[0] != "demo.mp3" {
		t.Fatalf("unexpected keywords: %#v", parsed.Keywords)
	}
}

func TestParseSearchRequestLimitType(t *testing.T) {
	var body bytes.Buffer
	_ = body.WriteByte(searchTypeLimit)
	_ = protocol.WriteUInt32(&body, 4096)
	_ = body.WriteByte(searchLimitMin)
	_ = protocol.WriteUInt16(&body, 1)
	_ = body.WriteByte(protocol.FTFileSize)

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse limit search: %v", err)
	}
	if parsed.MinSize != 4096 {
		t.Fatalf("expected min size 4096, got %d", parsed.MinSize)
	}
}

func TestParseSearchRequestIgnoresUnknownStringTag(t *testing.T) {
	var body bytes.Buffer
	_ = body.WriteByte(searchTypeStrTag)
	writeSearchStringValue(&body, "ignored")
	_ = protocol.WriteUInt16(&body, 1)
	_ = body.WriteByte(0xEE)
	_ = body.WriteByte(searchTypeString)
	writeSearchStringValue(&body, "visible")

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse unknown tag search: %v", err)
	}
	if len(parsed.Keywords) != 1 || parsed.Keywords[0] != "visible" {
		t.Fatalf("unexpected keywords: %#v", parsed.Keywords)
	}
}

func TestMatchesRecordKeywordAlternatives(t *testing.T) {
	record := FileRecord{Name: "movie.mkv", Size: 100}
	query := SearchQuery{KeywordAlternatives: []string{"avi", "mkv"}}
	if !matchesRecord(record, query) {
		t.Fatal("expected alternative keyword match")
	}
	query = SearchQuery{KeywordAlternatives: []string{"avi", "mp4"}}
	if matchesRecord(record, query) {
		t.Fatal("expected alternative keyword mismatch")
	}
}

func TestMatchesRecordExcludedKeywords(t *testing.T) {
	record := FileRecord{Name: "ubuntu-demo.iso", Size: 100}
	query := SearchQuery{Keywords: []string{"ubuntu"}, ExcludedKeywords: []string{"demo"}}
	if matchesRecord(record, query) {
		t.Fatal("expected excluded keyword to filter result")
	}
}

func writeSearchBool(buf *bytes.Buffer, operator byte) {
	_ = buf.WriteByte(searchTypeBool)
	_ = buf.WriteByte(operator)
}

func writeSearchStringTerm(buf *bytes.Buffer, value string) {
	_ = buf.WriteByte(searchTypeString)
	writeSearchStringValue(buf, value)
}

func writeSearchStringValue(buf *bytes.Buffer, value string) {
	_ = protocol.WriteUInt16(buf, uint16(len(value)))
	_, _ = buf.WriteString(value)
}

func TestParseSearchRequestStillMatchesGoed2K(t *testing.T) {
	request := serverproto.SearchRequest{
		Query:              "ubuntu iso",
		MinSize:            1024,
		MaxSize:            8192,
		MinSources:         5,
		MinCompleteSources: 3,
		FileType:           "Iso",
		Extension:          "iso",
	}
	var buf bytes.Buffer
	if err := request.Put(&buf); err != nil {
		t.Fatalf("put search request: %v", err)
	}
	parsed, err := ParseSearchRequest(buf.Bytes())
	if err != nil {
		t.Fatalf("parse search request: %v", err)
	}
	if parsed.FileType != "Iso" || parsed.Extension != "iso" {
		t.Fatalf("unexpected tag filters: %#v", parsed)
	}
	if parsed.MinSize != 1024 || parsed.MaxSize != 8192 {
		t.Fatalf("unexpected size filters: %#v", parsed)
	}
	if len(parsed.Keywords) != 2 || parsed.Keywords[0] != "ubuntu" || parsed.Keywords[1] != "iso" {
		t.Fatalf("unexpected keywords: %#v", parsed.Keywords)
	}
}

func TestParseSearchRequestTreeNumericEqualSize(t *testing.T) {
	var body bytes.Buffer
	_ = body.WriteByte(searchTypeLimit)
	_ = protocol.WriteUInt32(&body, 2048)
	_ = body.WriteByte(searchOpEqual)
	_ = protocol.WriteUInt16(&body, 1)
	_ = body.WriteByte(protocol.FTFileSize)

	parsed, err := ParseSearchRequest(body.Bytes())
	if err != nil {
		t.Fatalf("parse tree numeric equal: %v", err)
	}
	if parsed.MinSize != 2048 || parsed.MaxSize != 2048 {
		t.Fatalf("unexpected size filters: %#v", parsed)
	}
}

func TestMergeAndSearchQueriesDoesNotAliasLeft(t *testing.T) {
	left := SearchQuery{Keywords: []string{"a"}}
	right := SearchQuery{Keywords: []string{"b"}}
	merged := mergeAndSearchQueries(left, right)
	_ = mergeAndSearchQueries(left, SearchQuery{Keywords: []string{"c"}})
	if len(left.Keywords) != 1 || left.Keywords[0] != "a" {
		t.Fatalf("merge mutated left: %#v", left.Keywords)
	}
	if len(merged.Keywords) != 2 {
		t.Fatalf("unexpected merged keywords: %#v", merged.Keywords)
	}
}
