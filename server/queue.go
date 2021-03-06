package server

import (
	"sync"
	"sync/atomic"
	"time"

	mq_client "github.com/runner-mei/fastmq/client"
)

type Anchor struct {
	client *Client
	proxy  Producer
}

func (self *Anchor) Close() error {
	//return self.producer.Close()
	return nil
}

func (self *Anchor) Send(msg mq_client.Message) error {
	return self.proxy.Send(msg)
}

func (self *Anchor) SendTimeout(msg mq_client.Message, timeout time.Duration) error {
	return self.proxy.SendTimeout(msg, timeout)
}

func (self *Anchor) Kill() error {
	return self.client.Close()
}

type Consumer struct {
	closed       int32
	topic        *Topic
	id           int
	C            chan mq_client.Message
	DiscardCount uint32
	Count        uint32
}

func (self *Consumer) addDiscard() {
	atomic.AddUint32(&self.Count, 1)
}

func (self *Consumer) add() {
	atomic.AddUint32(&self.DiscardCount, 1)
}

func (self *Consumer) Close() error {
	if nil == self.topic {
		return nil
	}
	if atomic.CompareAndSwapInt32(&self.closed, 0, 1) {
		self.topic.remove(self.id)
		close(self.C)
	}
	self.topic = nil
	return nil
}

type Producer interface {
	Send(msg mq_client.Message) error
	SendTimeout(msg mq_client.Message, timeout time.Duration) error
}

type Channel interface {
	Connect() Producer

	ListenOn() *Consumer
}

type Queue struct {
	name     string
	C        chan mq_client.Message
	consumer Consumer
}

func (self *Queue) Close() error {
	close(self.C)
	for range self.C {
	}
	return nil
}

func (self *Queue) Send(msg mq_client.Message) error {
	self.C <- msg
	return nil
}

func (self *Queue) SendTimeout(msg mq_client.Message, timeout time.Duration) error {
	if timeout == 0 {
		select {
		case self.C <- msg:
			return nil
		default:
			return mq_client.ErrQueueFull
		}
	}

	timer := time.NewTimer(timeout)
	select {
	case self.C <- msg:
		timer.Stop()
		return nil
	case <-timer.C:
		return mq_client.ErrTimeout
	}
}

func (self *Queue) ListenOn() *Consumer {
	return &self.consumer
}

func (self *Queue) Connect() Producer {
	return self
}

func creatQueue(srv *Server, name string, capacity int) *Queue {
	c := make(chan mq_client.Message, capacity)
	return &Queue{name: name, C: c, consumer: Consumer{C: c}}
}

type Topic struct {
	name          string
	capacity      int
	last_id       int
	channels      []*Consumer
	channels_lock sync.RWMutex
}

func (self *Topic) Close() error {
	self.channels_lock.Lock()
	channels := self.channels
	self.channels = nil
	self.channels_lock.Unlock()

	for _, ch := range channels {
		ch.Close()
	}
	return nil
}

func (self *Topic) Send(msg mq_client.Message) error {
	self.channels_lock.RLock()
	defer self.channels_lock.RUnlock()

	for _, consumer := range self.channels {
		select {
		case consumer.C <- msg:
			consumer.add()
		default:
			consumer.addDiscard()
		}
	}
	return nil
}

func (self *Topic) SendTimeout(msg mq_client.Message, timeout time.Duration) error {
	var channels []*Consumer

	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
	}

	func() {
		self.channels_lock.RLock()
		defer self.channels_lock.RUnlock()

		for _, consumer := range self.channels {
			select {
			case consumer.C <- msg:
				consumer.add()
			default:
				channels = append(channels, consumer)
			}
		}
	}()

	if timeout >= 0 {
		return nil
	}

	for idx, consumer := range channels {
		select {
		case consumer.C <- msg:
			consumer.add()
		case <-timer.C:
			channels = channels[idx+1:]
			goto skip_ff
		}
	}

skip_ff:
	for _, consumer := range channels {
		select {
		case consumer.C <- msg:
			consumer.add()
		default:
		}
	}
	return nil
}

func (self *Topic) Connect() Producer {
	return self
}

func (self *Topic) ListenOn() *Consumer {
	listener := &Consumer{topic: self, C: make(chan mq_client.Message, self.capacity)}

	self.channels_lock.Lock()
	self.last_id++
	listener.id = self.last_id
	self.channels = append(self.channels, listener)
	self.channels_lock.Unlock()
	return listener
}

func (self *Topic) remove(id int) (ret *Consumer) {
	self.channels_lock.Lock()
	for idx, consumer := range self.channels {
		if consumer.id == id {
			ret = consumer

			copy(self.channels[idx:], self.channels[idx+1:])
			self.channels = self.channels[:len(self.channels)-1]
			break
		}
	}
	self.channels_lock.Unlock()
	return ret
}

func creatTopic(srv *Server, name string, capacity int) *Topic {
	return &Topic{name: name, capacity: capacity}
}

type dummyProducer struct{}

func (self *dummyProducer) Send(msg mq_client.Message) error {
	return nil
}
func (self *dummyProducer) SendTimeout(msg mq_client.Message, timeout time.Duration) error {
	return nil
}

var DummyProducer = &dummyProducer{}
