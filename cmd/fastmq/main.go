package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	mq_client "github.com/runner-mei/fastmq/client"
	"github.com/runner-mei/fastmq/server"

	"github.com/runner-mei/command"
)

func main() {
	command.ParseAndRun()
}

type runCmd struct {
}

func (self *runCmd) Flags(fs *flag.FlagSet) *flag.FlagSet {
	return fs
}

func (self *runCmd) Run(args []string) error {
	opt := &server.Options{HttpEnabled: true}

	srv, err := server.NewServer(opt)
	if err != nil {
		return err
	}
	defer srv.Close()

	srv.Wait()
	return nil
}

type sendCmd struct {
	address string
	typ     string
	id      string
	repeat  uint
	stat    bool
}

func (self *sendCmd) Flags(fs *flag.FlagSet) *flag.FlagSet {
	fs.StringVar(&self.address, "address", "127.0.0.1:4150", "the address of target mq server.")
	fs.StringVar(&self.typ, "type", "queue", "send to topic or queue.")
	fs.StringVar(&self.id, "id", "", "the name of client.")
	fs.UintVar(&self.repeat, "repeat", 1, "send message count.")
	fs.BoolVar(&self.stat, "stat", false, "stat message rate.")
	return fs
}

func (self *sendCmd) Run(args []string) error {
	if len(args) != 2 {
		return errors.New("arguments error!\r\nUsage: fastmq send queuname messagebody")
	}
	// if self.typ != "queue" && self.typ != "topic"  {
	// 	log.Fatalln("arguments error: type must is 'queue' or 'topic'.")
	// 	return
	// }

	cliBuilder := mq_client.Connect("", self.address)

	if self.id != "" {
		cliBuilder.Id(self.id)
	}

	var err error
	var cli *mq_client.SimplePubClient
	switch self.typ {
	case "topic":
		cli, err = cliBuilder.ToTopic(args[0])
	case "queue":
		cli, err = cliBuilder.ToQueue(args[0])
	default:
		return errors.New("arguments error: type must is 'queue' or 'topic'.")
	}

	if nil != err {
		return err
	}
	defer cli.Close()

	if self.repeat == 0 {
		self.repeat = 1
	}

	if self.stat {
		begin := mq_client.NewMessageWriter(mq_client.MSG_DATA, len(args[1])+1).Append([]byte("begin")).Build()
		end := mq_client.NewMessageWriter(mq_client.MSG_DATA, len(args[1])+1).Append([]byte("end")).Build()

		cli.Send(begin)

		for i := uint(0); i < self.repeat; i++ {
			msg := mq_client.NewMessageWriter(mq_client.MSG_DATA, len(args[1])+1).Append([]byte(args[1] + strconv.FormatUint(uint64(i), 10))).Build()
			cli.Send(msg)
		}

		cli.Send(end)
	} else {
		msg := mq_client.NewMessageWriter(mq_client.MSG_DATA, len(args[1])+1).Append([]byte(args[1])).Build()
		for i := uint(0); i < self.repeat; i++ {
			cli.Send(msg)
		}
	}
	cli.Stop()
	return nil
}

type subscribeCmd struct {
	address string
	typ     string
	id      string
	forward string
	console bool
	stat    bool
	//repeat  uint
}

func (self *subscribeCmd) Flags(fs *flag.FlagSet) *flag.FlagSet {
	fs.StringVar(&self.address, "address", "127.0.0.1:4150", "the address of target mq server.")
	fs.StringVar(&self.typ, "type", "queue", "send to topic or queue.")
	fs.StringVar(&self.id, "id", "", "the name of client.")
	fs.StringVar(&self.forward, "forward", "", "resend to address.")
	fs.BoolVar(&self.console, "console", true, "print message to console.")
	fs.BoolVar(&self.stat, "stat", false, "stat message rate.")
	//fs.UintVar(&self.repeat, "repeat", 1, "send message count.")
	return fs
}

func (self *subscribeCmd) Run(args []string) error {
	if len(args) != 1 {
		return errors.New("arguments error!\r\nUsage: fastmq subscribe queue name")
	}
	// if self.typ != "queue" && self.typ != "topic"  {
	// 	log.Fatalln("arguments error: type must is 'queue' or 'topic'.")
	// 	return
	// }

	var err error
	var forward *mq_client.SimplePubClient

	if self.forward != "" {
		forwardBuilder := mq_client.Connect("", self.address)

		if self.id != "" {
			forwardBuilder.Id(self.id + ".forward")
		}

		switch self.typ {
		case "topic":
			forward, err = forwardBuilder.ToTopic(self.forward)
		case "queue":
			forward, err = forwardBuilder.ToQueue(self.forward)
		default:
			return errors.New("arguments error: type must is 'queue' or 'topic'.")
		}

		if err != nil {
			return err
		}
	}

	subBuilder := mq_client.Connect("", self.address)

	if self.id != "" {
		subBuilder.Id(self.id)
	}

	var start_at, end_at time.Time
	var message_count uint = 0

	cb := func(cli *mq_client.Subscription, msg mq_client.Message) {
		if self.console {
			fmt.Println(string(msg.Data()))
		}

		if forward != nil {
			forward.Send(msg)
		}

		if self.stat {
			if bytes.Equal(msg.Data(), []byte("begin")) {
				//fmt.Println("recv:", message_count, ", elapsed:", time.Now().Sub(start_at))

				start_at = time.Now()
				message_count = 0
			} else if bytes.Equal(msg.Data(), []byte("end")) {
				end_at = time.Now()
				fmt.Println("recv:", message_count, ", elapsed:", end_at.Sub(start_at))
			} else {
				message_count++
			}
		}
	}

	switch self.typ {
	case "topic":
		return subBuilder.SubscribeTopic(args[0], cb)
	case "queue":
		return subBuilder.SubscribeQueue(args[0], cb)
	default:
		return errors.New("arguments error: type must is 'queue' or 'topic'.")
	}
}

func init() {
	command.On("run", "run as mq server", &runCmd{}, nil)
	command.On("send", "send messages to mq server", &sendCmd{}, nil)
	command.On("subscribe", "subscribe messages from mq server", &subscribeCmd{}, nil)
}
