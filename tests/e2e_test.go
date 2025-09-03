package tests

import (
	"context"
	"fmt"
	smtpmock "github.com/mocktools/go-smtp-mock/v2"
	sfm "github.com/termermc/streamfleet-mail"
	"github.com/wneessen/go-mail"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/termermc/streamfleet"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const TestQueue1 = "test-queue-1"

func prepareRedis(ctx context.Context) (cont testcontainers.Container, addr string, err error) {
	port := strconv.FormatInt(6380+rand.Int64N(25), 10)

	req := testcontainers.ContainerRequest{
		Image:        "docker.io/redis:8.2.1-alpine3.22",
		ExposedPorts: []string{port + ":6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	cont, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to start Redis container: %w", err)
	}

	return cont, "127.0.0.1:" + port, nil
}

func prepareSmtp() (server *smtpmock.Server, err error) {
	server = smtpmock.New(smtpmock.ConfigurationAttr{
		IsCmdFailFast: true,
	})
	err = server.Start()
	if err != nil {
		return nil, fmt.Errorf(`failed to start SMTP mock server: %w`, err)
	}

	return server, nil
}

type dependencies struct {
	Container testcontainers.Container
	RedisAddr string
	Client    *sfm.Client
	Worker    *sfm.Worker
	Smtp      *smtpmock.Server
}

func (d *dependencies) Close() {
	_ = d.Client.Close()
	_ = d.Worker.Close()
	_ = d.Smtp.Start()

	// Give time for server and client to close.
	time.Sleep(1 * time.Second)
	_ = d.Container.Stop(context.Background(), nil)
}

func mkDeps(opts ...any) (*dependencies, error) {
	ctx := context.Background()
	cont, redisAddr, err := prepareRedis(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to start Redis container: %w", err)
	}

	var redisOpt streamfleet.ToRedisClient
	var clientOpt streamfleet.ClientOpt
	var serverOpt streamfleet.ServerOpt
	for _, optAny := range opts {
		if rClientOpt, is := optAny.(streamfleet.RedisClientOpt); is {
			if rClientOpt.Addr == "" {
				rClientOpt.Addr = redisAddr
			}
			redisOpt = rClientOpt
		} else if failoverClientOpt, is := optAny.(streamfleet.RedisClientOpt); is {
			if failoverClientOpt.Addr == "" {
				failoverClientOpt.Addr = redisAddr
			}
			redisOpt = failoverClientOpt
		} else if clusterClientOpt, is := optAny.(streamfleet.RedisClusterClientOpt); is {
			if clusterClientOpt.Addrs == nil {
				clusterClientOpt.Addrs = []string{redisAddr}
			}
			redisOpt = clusterClientOpt
		} else if theClientOpt, is := optAny.(streamfleet.ClientOpt); is {
			clientOpt = theClientOpt
		} else if theServerOpt, is := optAny.(streamfleet.ServerOpt); is {
			serverOpt = theServerOpt
		}
	}

	if redisOpt == nil {
		redisOpt = streamfleet.RedisClientOpt{
			Addr: redisAddr,
		}
	}

	if clientOpt.RedisOpt == nil {
		clientOpt.RedisOpt = redisOpt
	}

	if serverOpt.ServerUniqueId == "" {
		serverOpt.ServerUniqueId = "testmailer.localhost"
	}
	if serverOpt.RedisOpt == nil {
		serverOpt.RedisOpt = redisOpt
	}

	client, err := sfm.NewClient(ctx, clientOpt, TestQueue1)
	if err != nil {
		return nil, err
	}

	smtp, err := prepareSmtp()
	if err != nil {
		return nil, err
	}

	mailClient, err := mail.NewClient("127.0.0.1",
		mail.WithPort(smtp.PortNumber()),
		mail.WithHELO("127.0.0.1"),
		mail.WithTLSPolicy(mail.NoTLS),
	)
	if err != nil {
		return nil, err
	}

	worker, err := sfm.NewWorker(ctx, mailClient, serverOpt, TestQueue1)
	if err != nil {
		return nil, err
	}

	return &dependencies{
		Container: cont,
		RedisAddr: redisAddr,
		Client:    client,
		Worker:    worker,
		Smtp:      smtp,
	}, nil
}

func mkTestMsg() *mail.Msg {
	msg := mail.NewMsg()
	err := msg.From("huangjin@irs.gov")
	if err != nil {
		panic(err)
	}
	err = msg.To("poorman@gmail.com")
	if err != nil {
		panic(err)
	}
	msg.Subject("Money Request")
	msg.SetBodyString(mail.TypeTextPlain, "We need all your money. Now.")

	return msg
}

const defaultMaxRetries = 5

func TestSimpleEnqueueAndForget(t *testing.T) {
	d, err := mkDeps()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	msg := mkTestMsg()

	err = d.Client.EnqueueAndForget(msg, streamfleet.TaskOpt{
		MaxRetries: defaultMaxRetries,
	})
	if err != nil {
		t.Fatal(err)
	}

	finChan := make(chan error, 1)

	go func() {
		err := d.Worker.Run()
		if err != nil {
			finChan <- err
		}
	}()
	go func() {
		msgs, err := d.Smtp.WaitForMessagesAndPurge(1, 10*time.Second)
		if err != nil {
			finChan <- err
			return
		}

		if len(msgs) != 1 {
			finChan <- fmt.Errorf("expected 1 message, got %d", len(msgs))
			return
		}

		finChan <- nil
	}()

	if err = <-finChan; err != nil {
		t.Fatal(err)
	}
}

func TestSimpleEnqueueAndTrack(t *testing.T) {
	d, err := mkDeps()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	msg := mkTestMsg()

	handle, err := d.Client.EnqueueAndTrack(msg, streamfleet.TaskOpt{
		MaxRetries: defaultMaxRetries,
	})
	if err != nil {
		t.Fatal(err)
	}

	finChan := make(chan error, 1)

	go func() {
		err := d.Worker.Run()
		if err != nil {
			finChan <- err
		}
	}()
	go func() {
		msgs, err := d.Smtp.WaitForMessagesAndPurge(1, 10*time.Second)
		if err != nil {
			finChan <- err
			return
		}

		if len(msgs) != 1 {
			finChan <- fmt.Errorf("expected 1 message, got %d", len(msgs))
			return
		}

		finChan <- nil
	}()

	// Wait for email to be sent to SMTP.
	if err = handle.Wait(); err != nil {
		t.Fatal(err)
	}

	if err = <-finChan; err != nil {
		t.Fatal(err)
	}
}

func TestFailingTaskTracked(t *testing.T) {
	d, err := mkDeps()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Message should fail because it has none of the required fields.
	msg := mail.NewMsg()

	handle, err := d.Client.EnqueueAndTrack(msg, streamfleet.TaskOpt{
		MaxRetries: defaultMaxRetries,
	})
	if err != nil {
		t.Fatal(err)
	}

	finChan := make(chan error, 1)

	go func() {
		err := d.Worker.Run()
		if err != nil {
			finChan <- err
		}
	}()

	// Wait for email to be sent to SMTP.
	if err = handle.Wait(); err == nil {
		t.Fatalf("expected handle to return error, but returned nil")
	}

	select {
	case err = <-finChan:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}
}
