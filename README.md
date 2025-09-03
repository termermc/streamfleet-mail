# streamfleet-mail
Redis-backed resilient email queue for Go.

The library is tightly coupled with Streamfleet, which provides a work queue that is tolerant to network loss and supports Redis clusters.

Instead of reinventing the wheel, the library wraps around [go-mail](https://github.com/wneessen/go-mail) to provide email/SMTP functionality.
It simply provides the client-worker queue system for facilitating sending emails.

## Features

 - Fully-featured email support via [go-mail](https://github.com/wneessen/go-mail)
 - Support for Redis Sentinel and Cluster
 - Automatic retry and worker crash recovery
 - Tolerance for Redis downtime without data loss

## Architecture

The library is broken up into two components, coordinated by Redis:

 - The client, which submits (enqueues) emails to the queue.

 - The worker, which accepts emails from the queue and sends them with a configured email client.

   In addition to receiving new emails, workers can claim emails that have sat idle too long in other workers' pending lists.
   Only workers that have crashed or are unresponsive would have their emails reclaimed.

   If an email fails to send while the worker is running, the worker will put the email back on the queue and increment its failure count.

Note that workers and clients may exist on the same process; they do not need to be in separate microservices.

If the underlying Redis server is unavailable, the clients will wait until it comes back online.
Emails submitted to the client stay queued in-memory until Redis is available.

To read more about how the underlying queue works, see [Streamfleet](https://github.com/termermc/streamfleet)'s documentation.

## Use It

To use the library, it is recommended to add it and [go-mail](https://github.com/wneessen/go-mail) to your project:

```bash
go get https://github.com/termermc/streamfleet-mail
go get https://github.com/wneessen/go-mail
```

To see some examples, visit the [examples](./examples) directory.

### Simple Example

```go
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
```

## Acknowledgements
I would like to sincerely thank [Vladislav Trotsenko](https://github.com/bestwebua) for creating [go-smtp-mock](https://github.com/mocktools/go-smtp-mock), which made creating integration tests for this library much easier.
