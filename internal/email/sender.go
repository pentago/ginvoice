package email

import "context"

// Sender sends a single email with an optional attachment.
type Sender interface {
	Send(ctx context.Context, to string, cc []string, subject, html, attachmentName string, attachmentBytes []byte) error
}

// FakeSender records calls in-memory for test assertions. Thread-safe for basic test use.
type FakeSender struct {
	Calls []Call
	Err   error // if set, Send returns this error
}

type Call struct {
	To             string
	CC             []string
	Subject        string
	HTML           string
	AttachmentName string
	AttachmentLen  int
}

func (f *FakeSender) Send(_ context.Context, to string, cc []string, subject, html, attachmentName string, attachmentBytes []byte) error {
	if f.Err != nil {
		return f.Err
	}
	f.Calls = append(f.Calls, Call{
		To:             to,
		CC:             cc,
		Subject:        subject,
		HTML:           html,
		AttachmentName: attachmentName,
		AttachmentLen:  len(attachmentBytes),
	})
	return nil
}
