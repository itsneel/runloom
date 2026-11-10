package definition

import (
	"fmt"
	"github.com/example/runloom/workflows/internal/model"
)

func ResolveEmail(message model.EmailMessage, values map[string]any) (model.EmailMessage, error) {
	resolve := func(value string) (string, error) {
		resolved, err := Resolve(value, values)
		if err != nil {
			return "", err
		}
		text, ok := resolved.(string)
		if !ok {
			return "", fmt.Errorf("email fields must resolve to text")
		}
		return text, nil
	}
	list := func(values []string) ([]string, error) {
		result := make([]string, len(values))
		for i, value := range values {
			text, err := resolve(value)
			if err != nil {
				return nil, err
			}
			result[i] = text
		}
		return result, nil
	}
	var err error
	message.To, err = list(message.To)
	if err != nil {
		return message, err
	}
	message.CC, err = list(message.CC)
	if err != nil {
		return message, err
	}
	message.BCC, err = list(message.BCC)
	if err != nil {
		return message, err
	}
	for _, field := range []*string{&message.Subject, &message.Text, &message.HTML, &message.ReplyTo} {
		*field, err = resolve(*field)
		if err != nil {
			return message, err
		}
	}
	// Sender.Send validates resolved addresses, headers and body limits inside
	// the Activity before any SMTP connection. Keep net/mail out of replay code.
	return message, nil
}
