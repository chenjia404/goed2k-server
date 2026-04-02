package ed2ksrv

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	opLoginRequest   byte = 0x01
	opGetServerList  byte = 0x14
	opSearchRequest  byte = 0x16
	opGetSources     byte = 0x19
	opGetSourcesObfu byte = 0x23 // OP_GETSOURCES_OBFU，与 OP_GETSOURCES 请求体相同，应答需用 OP_FOUNDSOURCES_OBFU
	opCallbackReq    byte = 0x1C
	opSearchMore     byte = 0x21
	opFoundSourcesObfu byte = 0x44 // OP_FOUNDSOURCES_OBFU，每源在 IP:端口后多 1 字节 crypt options
	searchTypeBool   byte = 0x00
	searchTypeString byte = 0x01
	searchTypeStrTag byte = 0x02
	searchTypeUint32 byte = 0x03
	searchTypeUint64 byte = 0x08
	searchOpEqual    byte = 0x00
	searchOpGreater  byte = 0x01
	searchOpLess     byte = 0x02
)

// SearchQuery is the server-side view of an incoming ED2K search request.
type SearchQuery struct {
	Keywords           []string
	MinSize            int64
	MaxSize            int64
	MinSources         int
	MinCompleteSources int
	FileType           string
	Extension          string
}

// ParseSearchRequest decodes the subset of OP_SEARCHREQ emitted by goed2k.
func ParseSearchRequest(body []byte) (SearchQuery, error) {
	reader := bytes.NewReader(body)
	query := SearchQuery{}
	for reader.Len() > 0 {
		termType, err := reader.ReadByte()
		if err != nil {
			return SearchQuery{}, err
		}
		switch termType {
		case searchTypeBool:
			// The reference client only emits AND operators; the server can safely skip them.
			if _, err := reader.ReadByte(); err != nil {
				return SearchQuery{}, err
			}
		case searchTypeString:
			value, err := readSearchString(reader)
			if err != nil {
				return SearchQuery{}, err
			}
			if value != "" {
				query.Keywords = append(query.Keywords, value)
			}
		case searchTypeStrTag:
			value, tagID, err := readSearchStringTag(reader)
			if err != nil {
				return SearchQuery{}, err
			}
			switch tagID {
			case protocol.FTFileType:
				query.FileType = value
			case protocol.FTFileFormat:
				query.Extension = value
			default:
				return SearchQuery{}, fmt.Errorf("unsupported string tag: 0x%02x", tagID)
			}
		case searchTypeUint32:
			value, err := protocol.ReadUInt32(reader)
			if err != nil {
				return SearchQuery{}, err
			}
			if err := applyNumericSearchTerm(&query, uint64(value), reader); err != nil {
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

func applyNumericSearchTerm(query *SearchQuery, value uint64, reader *bytes.Reader) error {
	operator, err := reader.ReadByte()
	if err != nil {
		return err
	}
	tagCount, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	if tagCount != 1 {
		return fmt.Errorf("unsupported numeric tag count: %d", tagCount)
	}
	tagID, err := reader.ReadByte()
	if err != nil {
		return err
	}
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
		default:
			return fmt.Errorf("unsupported file size operator: 0x%02x", operator)
		}
	case protocol.FTSources:
		if operator != searchOpGreater && operator != searchOpEqual {
			return fmt.Errorf("unsupported sources operator: 0x%02x", operator)
		}
		query.MinSources = int(value)
	case protocol.FTCompleteSources:
		if operator != searchOpGreater && operator != searchOpEqual {
			return fmt.Errorf("unsupported complete sources operator: 0x%02x", operator)
		}
		query.MinCompleteSources = int(value)
	default:
		return fmt.Errorf("unsupported numeric tag: 0x%02x", tagID)
	}
	return nil
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
	tagCount, err := protocol.ReadUInt16(reader)
	if err != nil {
		return "", 0, err
	}
	if tagCount != 1 {
		return "", 0, fmt.Errorf("unsupported string tag count: %d", tagCount)
	}
	tagID, err := reader.ReadByte()
	if err != nil {
		return "", 0, err
	}
	return value, tagID, nil
}
