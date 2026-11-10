package protection

import (
	"github.com/example/runloom/workflows/internal/config"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

const encoding = "binary/runloom-encrypted-v1"

type Codec struct{ Ring *Ring }

func (c Codec) Encode(values []*commonpb.Payload) ([]*commonpb.Payload, error) {
	out := make([]*commonpb.Payload, len(values))
	for i, p := range values {
		raw, err := proto.Marshal(p)
		if err != nil {
			return nil, ErrProtectedData
		}
		raw, err = c.Ring.Seal("temporal-payload/v1", raw)
		if err != nil {
			return nil, err
		}
		out[i] = &commonpb.Payload{Metadata: map[string][]byte{converter.MetadataEncoding: []byte(encoding)}, Data: raw}
	}
	return out, nil
}
func (c Codec) Decode(values []*commonpb.Payload) ([]*commonpb.Payload, error) {
	out := make([]*commonpb.Payload, len(values))
	for i, p := range values {
		if string(p.Metadata[converter.MetadataEncoding]) != encoding {
			if c.Ring != nil && config.Value("RUNLOOM_ALLOW_LEGACY_PAYLOADS") != "true" {
				return nil, ErrProtectedData
			}
			out[i] = p
			continue
		}
		raw, err := c.Ring.Open("temporal-payload/v1", p.Data)
		if err != nil {
			return nil, err
		}
		out[i] = &commonpb.Payload{}
		if proto.Unmarshal(raw, out[i]) != nil {
			return nil, ErrProtectedData
		}
	}
	return out, nil
}

func DataConverter() (converter.DataConverter, error) {
	ring, err := FromConfig()
	if err != nil {
		return nil, err
	}
	base := converter.GetDefaultDataConverter()
	if ring == nil {
		return base, nil
	}
	return converter.NewCodecDataConverter(base, Codec{ring}), nil
}
func TemporalOptions(address, namespace string) (client.Options, error) {
	if driver := config.Value("RUNLOOM_DATABASE_DRIVER"); driver != "" && driver != "file" && !Enabled() {
		return client.Options{}, ErrProtectedData
	}
	dc, err := DataConverter()
	if err != nil {
		return client.Options{}, err
	}
	tlsConfig, err := temporalTLS()
	if err != nil {
		return client.Options{}, err
	}
	// Encode common failure attributes as payloads too: messages and stack traces
	// can otherwise escape payload encryption into plaintext Temporal history.
	return client.Options{HostPort: address, Namespace: namespace, DataConverter: dc,
		ConnectionOptions: client.ConnectionOptions{TLS: tlsConfig},
		FailureConverter:  temporal.NewDefaultFailureConverter(temporal.DefaultFailureConverterOptions{DataConverter: dc, EncodeCommonAttributes: true}),
		Logger:            SafeLogger{},
	}, nil
}

// SafeLogger suppresses SDK free-text/error fields which may embed activity
// inputs or provider responses. Emit only bounded severity/categorical events.
type SafeLogger struct{}

func (SafeLogger) Debug(string, ...interface{}) {}
func (SafeLogger) Info(string, ...interface{})  {}
func (SafeLogger) Warn(string, ...interface{})  { safeLog("warning") }
func (SafeLogger) Error(string, ...interface{}) { safeLog("error") }

// Enabled lets storage tests and operators distinguish plaintext native dev.
func Enabled() bool {
	return config.Value("RUNLOOM_PAYLOAD_KEYRING_FILE") != "" || config.Value("RUNLOOM_SECRETS_KEY_FILE") != ""
}
