package ed2ksrv

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	opLoginRequest     byte = 0x01
	opGetServerList    byte = 0x14
	opSearchRequest    byte = 0x16
	opSearchUser       byte = 0x1A
	opSearchUserResults byte = 0x43
	opGetSources       byte = 0x19
	opGetSourcesObfu   byte = 0x23
	opCallbackReq      byte = 0x1C
	opSearchMore       byte = 0x21
	opFoundSourcesObfu byte = 0x44
	searchTypeBool     byte = 0x00
	searchTypeString   byte = 0x01
	searchTypeStrTag   byte = 0x02
	searchTypeLimit    byte = 0x03
	searchTypeUint64   byte = 0x08
	searchOpEqual      byte = 0x00
	searchOpGreater    byte = 0x01
	searchOpLess       byte = 0x02
	searchLimitMin     byte = 0x01
	searchLimitMax     byte = 0x02
	ftMediaTitle       byte = 0xD6
	ftMediaAlbum       byte = 0xD7
	ftMediaArtist      byte = 0xD8
)

// SearchQuery is the server-side view of an incoming ED2K search request.
type SearchQuery struct {
	Keywords            []string
	KeywordAlternatives []string
	ExcludedKeywords    []string
	MinSize             int64
	MaxSize             int64
	MinSources          int
	MinCompleteSources  int
	FileType            string
	Extension           string
}

// ParseSearchRequest decodes ED2K OP_SEARCHREQ bodies from goed2k、eMule、aMule 等客户端。
// eMule 使用递归布尔树（首字节常为 0x00），goed2k 使用扁平 AND 链（首字节常为 0x02）。
func ParseSearchRequest(body []byte) (SearchQuery, error) {
	if len(body) == 0 {
		return SearchQuery{}, nil
	}
	if body[0] == searchTypeBool {
		if query, err := parseSearchTree(bytes.NewReader(body)); err == nil {
			return query, nil
		}
	}
	if query, err := parseSearchFlat(bytes.NewReader(body)); err == nil {
		return query, nil
	}
	if body[0] != searchTypeBool {
		if query, err := parseSearchTree(bytes.NewReader(body)); err == nil {
			return query, nil
		}
	}
	return parseSearchFlat(bytes.NewReader(body))
}

func parseSearchFlat(reader *bytes.Reader) (SearchQuery, error) {
	query := SearchQuery{}
	for reader.Len() > 0 {
		termType, err := reader.ReadByte()
		if err != nil {
			return SearchQuery{}, err
		}
		switch termType {
		case searchTypeBool:
			if _, err := reader.ReadByte(); err != nil {
				return SearchQuery{}, err
			}
		case searchTypeString:
			value, err := readSearchString(reader)
			if err != nil {
				return SearchQuery{}, err
			}
			appendKeyword(&query, value)
		case searchTypeStrTag:
			if err := consumeSearchStringTag(reader, &query); err != nil {
				return SearchQuery{}, err
			}
		case searchTypeLimit:
			if err := consumeSearchNumeric(reader, &query); err != nil {
				return SearchQuery{}, err
			}
		case searchTypeUint64:
			value, err := protocol.ReadUInt64(reader)
			if err != nil {
				return SearchQuery{}, err
			}
			if err := applyNumericSearchTerm(&query, value, reader); err != nil {
				return SearchQuery{}, err
			}
		default:
			return SearchQuery{}, fmt.Errorf("unsupported search term type: 0x%02x", termType)
		}
	}
	return query, nil
}

func parseSearchTree(reader *bytes.Reader) (SearchQuery, error) {
	termType, err := reader.ReadByte()
	if err != nil {
		return SearchQuery{}, err
	}
	switch termType {
	case searchTypeBool:
		operator, err := reader.ReadByte()
		if err != nil {
			return SearchQuery{}, err
		}
		left, err := parseSearchTree(reader)
		if err != nil {
			return SearchQuery{}, err
		}
		right, err := parseSearchTree(reader)
		if err != nil {
			return SearchQuery{}, err
		}
		return mergeSearchQueries(left, right, operator), nil
	case searchTypeString:
		value, err := readSearchString(reader)
		if err != nil {
			return SearchQuery{}, err
		}
		query := SearchQuery{}
		appendKeyword(&query, value)
		return query, nil
	case searchTypeStrTag:
		query := SearchQuery{}
		if err := consumeSearchStringTag(reader, &query); err != nil {
			return SearchQuery{}, err
		}
		return query, nil
	case searchTypeLimit:
		query := SearchQuery{}
		if err := consumeSearchNumeric(reader, &query); err != nil {
			return SearchQuery{}, err
		}
		return query, nil
	case searchTypeUint64:
		value, err := protocol.ReadUInt64(reader)
		if err != nil {
			return SearchQuery{}, err
		}
		query := SearchQuery{}
		if err := applyNumericSearchTerm(&query, value, reader); err != nil {
			return SearchQuery{}, err
		}
		return query, nil
	default:
		return SearchQuery{}, fmt.Errorf("unsupported search term type: 0x%02x", termType)
	}
}

func mergeSearchQueries(left, right SearchQuery, operator byte) SearchQuery {
	switch operator {
	case searchOpEqual:
		return mergeAndSearchQueries(left, right)
	case searchOpGreater:
		return mergeOrSearchQueries(left, right)
	case searchOpLess:
		merged := mergeAndSearchQueries(left, right)
		merged.ExcludedKeywords = append(merged.ExcludedKeywords, keywordSet(right)...)
		return merged
	default:
		return mergeAndSearchQueries(left, right)
	}
}

func mergeAndSearchQueries(left, right SearchQuery) SearchQuery {
	out := SearchQuery{
		Keywords:            append([]string(nil), left.Keywords...),
		KeywordAlternatives: append([]string(nil), left.KeywordAlternatives...),
		ExcludedKeywords:    append([]string(nil), left.ExcludedKeywords...),
		MinSize:             left.MinSize,
		MaxSize:             left.MaxSize,
		MinSources:          left.MinSources,
		MinCompleteSources:  left.MinCompleteSources,
		FileType:            left.FileType,
		Extension:           left.Extension,
	}
	out.Keywords = appendUniqueStrings(out.Keywords, right.Keywords...)
	out.KeywordAlternatives = appendUniqueStrings(out.KeywordAlternatives, right.KeywordAlternatives...)
	out.ExcludedKeywords = appendUniqueStrings(out.ExcludedKeywords, right.ExcludedKeywords...)
	if right.FileType != "" {
		out.FileType = right.FileType
	}
	if right.Extension != "" {
		out.Extension = right.Extension
	}
	if right.MinSize > out.MinSize {
		out.MinSize = right.MinSize
	}
	if right.MaxSize == 0 || (out.MaxSize > 0 && right.MaxSize < out.MaxSize) {
		if right.MaxSize > 0 {
			out.MaxSize = right.MaxSize
		}
	}
	if right.MinSources > out.MinSources {
		out.MinSources = right.MinSources
	}
	if right.MinCompleteSources > out.MinCompleteSources {
		out.MinCompleteSources = right.MinCompleteSources
	}
	return out
}

func mergeOrSearchQueries(left, right SearchQuery) SearchQuery {
	if left.isKeywordOnly() && right.isKeywordOnly() {
		return SearchQuery{KeywordAlternatives: appendUniqueStrings(keywordSet(left), keywordSet(right)...)}
	}
	return mergeAndSearchQueries(left, right)
}

func (q SearchQuery) isKeywordOnly() bool {
	return q.FileType == "" && q.Extension == "" && q.MinSize == 0 && q.MaxSize == 0 &&
		q.MinSources == 0 && q.MinCompleteSources == 0 && len(q.ExcludedKeywords) == 0
}

func keywordSet(q SearchQuery) []string {
	out := append([]string(nil), q.Keywords...)
	out = appendUniqueStrings(out, q.KeywordAlternatives...)
	return out
}

func appendKeyword(query *SearchQuery, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		query.Keywords = append(query.Keywords, value)
	}
}

func consumeSearchNumeric(reader *bytes.Reader, query *SearchQuery) error {
	value, err := protocol.ReadUInt32(reader)
	if err != nil {
		return err
	}
	operator, err := reader.ReadByte()
	if err != nil {
		return err
	}
	tagID, err := readSearchTagID(reader)
	if err != nil {
		return err
	}
	switch operator {
	case searchLimitMin:
		operator = searchOpGreater
	case searchLimitMax:
		operator = searchOpLess
	}
	return applyNumericTag(query, tagID, operator, uint64(value))
}

func consumeSearchStringTag(reader *bytes.Reader, query *SearchQuery) error {
	value, tagID, err := readSearchStringTag(reader)
	if err != nil {
		return err
	}
	applyStringSearchTag(query, tagID, value)
	return nil
}

func applyStringSearchTag(query *SearchQuery, tagID byte, value string) {
	value = strings.TrimSpace(value)
	switch tagID {
	case protocol.FTFilename:
		appendKeyword(query, value)
	case protocol.FTFileType:
		query.FileType = value
	case protocol.FTFileFormat:
		query.Extension = value
	case protocol.FTMediaCodec, ftMediaTitle, ftMediaAlbum, ftMediaArtist:
		appendKeyword(query, value)
	}
}

func applyNumericSearchTerm(query *SearchQuery, value uint64, reader *bytes.Reader) error {
	operator, err := reader.ReadByte()
	if err != nil {
		return err
	}
	tagID, err := readSearchTagID(reader)
	if err != nil {
		return err
	}
	return applyNumericTag(query, tagID, operator, value)
}

func applyNumericTag(query *SearchQuery, tagID, operator byte, value uint64) error {
	switch tagID {
	case protocol.FTFileSize:
		switch operator {
		case searchOpGreater:
			query.MinSize = int64(value)
		case searchOpLess:
			query.MaxSize = int64(value)
		case searchOpEqual:
			query.MinSize = int64(value)
			query.MaxSize = int64(value)
		}
	case protocol.FTSources:
		if operator == searchOpGreater || operator == searchOpEqual {
			query.MinSources = int(value)
		}
	case protocol.FTCompleteSources:
		if operator == searchOpGreater || operator == searchOpEqual {
			query.MinCompleteSources = int(value)
		}
	case protocol.FTMediaBitrate:
		if operator == searchOpGreater || operator == searchOpEqual {
			appendKeyword(query, fmt.Sprintf("%d", value))
		}
	case protocol.FTMediaLength:
		if operator == searchOpGreater || operator == searchOpEqual {
			appendKeyword(query, fmt.Sprintf("%d", value))
		}
	}
	return nil
}

func readSearchTagID(reader *bytes.Reader) (byte, error) {
	tagCount, err := protocol.ReadUInt16(reader)
	if err != nil {
		return 0, err
	}
	if tagCount == 0 {
		return 0, nil
	}
	tagID, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	for i := uint16(1); i < tagCount; i++ {
		if _, err := reader.ReadByte(); err != nil {
			return tagID, nil
		}
	}
	return tagID, nil
}

func readSearchString(reader *bytes.Reader) (string, error) {
	size, err := protocol.ReadUInt16(reader)
	if err != nil {
		return "", err
	}
	raw, err := protocol.ReadBytes(reader, int(size))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func readSearchStringTag(reader *bytes.Reader) (string, byte, error) {
	value, err := readSearchString(reader)
	if err != nil {
		return "", 0, err
	}
	tagID, err := readSearchTagID(reader)
	if err != nil {
		return "", 0, err
	}
	return value, tagID, nil
}

func appendUniqueStrings(base []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, existing := range base {
			if strings.EqualFold(existing, value) {
				found = true
				break
			}
		}
		if !found {
			base = append(base, value)
		}
	}
	return base
}
