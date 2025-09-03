package main

import (
	"context"
	"github.com/termermc/streamfleet"
	sfm "github.com/termermc/streamfleet-mail"
	"github.com/wneessen/go-mail"
)

func main() {
	var redisOpt = streamfleet.RedisClientOpt{
		Addr: "127.0.0.1:6379",
	}

	const MailQueueKey = "newsletter"

	ctx := context.Background()

	// Create the email client used by the worker.
	mailClient, err := mail.NewClient("smtp.example.com", mail.WithTLSPolicy(mail.TLSMandatory))
	if err != nil {
		panic(err)
	}

	// Create worker.
	// ServerUniqueId is used to differentiate workers from each other.
	// Some other options we could specify:
	//  - Logger (an implementation of slog.Logger from the standard library to override the default logger)
	//  - HandlerConcurrency (how many concurrent emails the worker can send, defaults to 1)
	// Take a look at the streamfleet.ServerOpt struct for a complete list of options.
	worker, err := sfm.NewWorker(ctx, mailClient, streamfleet.ServerOpt{
		ServerUniqueId: "node1.example.com",
		RedisOpt:       redisOpt,
	}, MailQueueKey)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = worker.Close()
	}()

	// Launch worker.
	// The Run() method blocks until the worker is closed or runs into a fatal error, so it is launched in its own goroutine.
	// Note that workers do not need to be created before clients; queued emails will be picked up as soon as a worker is available.
	go func() {
		err := worker.Run()
		if err != nil {
			panic(err)
		}
	}()

	// Create a client using the same Redis connection options as the worker.
	// Unlike workers, we do not need to manually provide a unique ID.
	// Instead, the client generates its own internally.
	// Take a look at the streamfleet.ClientOpt struct for a complete list of options.
	// Note that providing an email client is not necessary here, as workers manage their own.
	client, err := sfm.NewClient(ctx, streamfleet.ClientOpt{
		RedisOpt: redisOpt,
	}, MailQueueKey)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Create a message to send.
	msg := mail.NewMsg()
	_ = msg.From("huangjin@irs.gov")
	_ = msg.To("poorman@gmail.com")
	msg.Subject("Money Request")
	msg.SetBodyString(mail.TypeTextPlain, "We need all your money. Now.")

	// Send the email and let a worker handle it.
	// We can also choose to wait for a worker to accept and send the message by calling EnqueueAndTrack instead.
	err = client.EnqueueAndForget(msg, streamfleet.TaskOpt{})
	if err != nil {
		panic(err)
	}

	println("Email sent!")
}
