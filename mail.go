package streamfleet_mail

import (
	"bytes"
	"context"
	"fmt"

	"github.com/termermc/streamfleet"
	"github.com/wneessen/go-mail"
)

// QueueKeyPrefix is the prefix used for the underlying Streamfleet queues used for email queues.
const QueueKeyPrefix = "sfmail:"

// Client is a mail queue client.
type Client struct {
	sf       *streamfleet.Client
	queueKey string
}

// NewClient creates a new mail client.
// Uses the specified queue key and Streamfleet client options.
// The client will only be able to send mail on the mail queue with the specified key.
// Make sure all workers this client is meant to queue to have the same queue key.
func NewClient(ctx context.Context, sfClientOpt streamfleet.ClientOpt, queueKey string) (*Client, error) {
	key := QueueKeyPrefix + queueKey

	sf, err := streamfleet.NewClient(ctx, sfClientOpt, key)
	if err != nil {
		return nil, fmt.Errorf(`sfmail: failed to create underlying streamfleet client: %w`, err)
	}

	return &Client{
		sf:       sf,
		queueKey: key,
	}, nil
}

// EnqueueAndForget adds an email to the queue and immediately returns.
// Errors in queuing or the status of the email once it is picked up by a server are not tracked.
// If you need to know when the email has been sent, use EnqueueAndTrack.
// If the number of locally queued emails would exceed streamfleet.ClientOpt.MaxLocalQueueSize, returns streamfleet.ErrClientQueueFull.
// This will normally only happen if the client cannot connect to Redis and emails pile up.
func (c *Client) EnqueueAndForget(email *mail.Msg, opt streamfleet.TaskOpt) error {
	var buf bytes.Buffer
	if _, err := email.WriteTo(&buf); err != nil {
		return fmt.Errorf(`sfmail: failed to serialize email: %w`, err)
	}

	err := c.sf.EnqueueAndForget(c.queueKey, buf.String(), opt)
	if err != nil {
		return fmt.Errorf(`sfmail: failed to enqueue (and forget) email: %w`, err)
	}

	return nil
}

// EnqueueAndTrack adds an email to the queue and returns a handle to it.
// Errors in queuing or the status of the email once it is picked up by the server can be tracked using the returned handle.
// If you do not need to know when the email has been sent, use EnqueueAndForget.
// You should only use this method if you need to know the status of the email, as it is less efficient than EnqueueAndForget.
//
// If the number of locally queued emails would exceed streamfleet.ClientOpt.MaxLocalQueueSize, returns streamfleet.ErrClientQueueFull.
// This will normally only happen if the client cannot connect to Redis and emails pile up.
func (c *Client) EnqueueAndTrack(email *mail.Msg, opt streamfleet.TaskOpt) (*streamfleet.TaskHandle, error) {
	var buf bytes.Buffer
	if _, err := email.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf(`sfmail: failed to serialize email: %w`, err)
	}

	handle, err := c.sf.EnqueueAndTrack(c.queueKey, buf.String(), opt)
	if err != nil {
		return nil, fmt.Errorf(`sfmail: failed to enqueue (and track) email: %w`, err)
	}

	return handle, nil
}

// Close closes the client and releases all associated resources.
// The client cannot be used after Close has been called.
func (c *Client) Close() error {
	if err := c.sf.Close(); err != nil {
		return fmt.Errorf(`sfmail: failed to close underlying streamfleet client: %w`, err)
	}

	return nil
}

// Worker is a worker that receives emails from the queue and sends them.
type Worker struct {
	mc *mail.Client
	sf *streamfleet.Server

	queueKey string
}

// NewWorker creates a new mail worker that takes emails off the queue and sends them.
// Uses the specified queue key and Streamfleet server options.
// The worker will only be able to receive mail from the mail queue with the specified key.
// Make sure all clients this worker is meant to queue from have the same queue key.
func NewWorker(ctx context.Context, mailClient *mail.Client, sfClientOpt streamfleet.ServerOpt, queueKey string) (*Worker, error) {
	key := QueueKeyPrefix + queueKey

	sf, err := streamfleet.NewServer(ctx, sfClientOpt)
	if err != nil {
		return nil, fmt.Errorf(`sfmail: failed to create underlying streamfleet client: %w`, err)
	}

	w := &Worker{
		mc:       mailClient,
		sf:       sf,
		queueKey: key,
	}

	sf.Handle(key, w.handle)

	return w, nil
}

func (w *Worker) handle(ctx context.Context, task *streamfleet.Task) error {
	email, err := mail.EMLToMsgFromString(task.Data)
	if err != nil {
		return fmt.Errorf(`sfmail: failed to decode EML message from queue: %w`, err)
	}

	err = w.mc.DialAndSendWithContext(ctx, email)
	if err != nil {
		return fmt.Errorf(`sfmail: failed to dail and send email from queue: %w`, err)
	}

	return nil
}

// Run runs the email worker and blocks until Close() is returned or a fatal error is returned by the underlying Streamfleet server.
// This should usually be run in its own goroutine due to its blocking nature.
func (w *Worker) Run() error {
	if err := w.sf.Run(); err != nil {
		return fmt.Errorf(`sfmail: streamfleet server Run method returned error: %w`, err)
	}
	return nil
}

// Close closes the worker and releases all associated resources.
// The worker cannot be used after Close has been called.
func (w *Worker) Close() error {
	if err := w.mc.Close(); err != nil {
		return fmt.Errorf(`sfmail: failed to close underlying mail client: %w`, err)
	}
	if err := w.sf.Close(); err != nil {
		return fmt.Errorf(`sfmail: failed to close underlying streamfleet client: %w`, err)
	}

	return nil
}
