// Credit for The NATS.IO Authors
// Copyright 2021-2022 The Memphis Authors
// Licensed under the MIT License (the "License");
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// This license limiting reselling the software itself "AS IS".

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
package server

import (
	"context"
	"memphis-broker/models"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const CONN_STATUS_SUBJ = "$memphis_connection_status"

func killRelevantConnections(zombieConnections []primitive.ObjectID) error {
	_, err := connectionsCollection.UpdateMany(context.TODO(),
		bson.M{"_id": bson.M{"$in": zombieConnections}},
		bson.M{"$set": bson.M{"is_active": false}},
	)
	if err != nil {
		serv.Errorf("killRelevantConnections error: " + err.Error())
		return err
	}

	return nil
}

func killProducersByConnections(connectionIds []primitive.ObjectID) error {
	_, err := producersCollection.UpdateMany(context.TODO(),
		bson.M{"connection_id": bson.M{"$in": connectionIds}},
		bson.M{"$set": bson.M{"is_active": false}},
	)
	if err != nil {
		serv.Errorf("killProducersByConnections error: " + err.Error())
		return err
	}

	return nil
}

func killConsumersByConnections(connectionIds []primitive.ObjectID) error {
	_, err := consumersCollection.UpdateMany(context.TODO(),
		bson.M{"connection_id": bson.M{"$in": connectionIds}},
		bson.M{"$set": bson.M{"is_active": false}},
	)
	if err != nil {
		serv.Errorf("killConsumersByConnections error: " + err.Error())
		return err
	}

	return nil
}

func removeOldPoisonMsgs() error {
	filter := bson.M{"creation_date": bson.M{"$lte": (time.Now().Add(-(time.Hour * time.Duration(configuration.POISON_MSGS_RETENTION_IN_HOURS))))}}
	_, err := poisonMessagesCollection.DeleteMany(context.TODO(), filter)
	if err != nil {
		return err
	}

	return nil
}

func (srv *Server) removeRedundantStations() error {
	var stations []models.Station
	cursor, err := stationsCollection.Find(nil, bson.M{"is_deleted": false})
	if err != nil {
		return err
	}

	if err = cursor.All(nil, &stations); err != nil {
		return err
	}

	redundant := make([]string, 0, len(stations))
	for _, s := range stations {
		_, err = srv.memphisStreamInfo(s.Name)
		if IsNatsErr(err, JSStreamNotFoundErr) {
			redundant = append(redundant, s.Name)
		}
	}

	_, err = stationsCollection.UpdateMany(nil,
		bson.M{"name": bson.M{"$in": redundant}},
		bson.M{"$set": bson.M{"is_deleted": true}})
	return err
}

func getActiveConnections() ([]models.Connection, error) {
	var connections []models.Connection
	cursor, err := connectionsCollection.Find(context.TODO(), bson.M{"is_active": true})
	if err != nil {
		return connections, err
	}
	if err = cursor.All(context.TODO(), &connections); err != nil {
		return connections, err
	}

	return connections, nil
}

func (s *Server) ListenForZombieConnCheckRequests() error {
	_, err := s.subscribeOnGlobalAcc(CONN_STATUS_SUBJ, CONN_STATUS_SUBJ+"_sid", func(_ *client, subject, reply string, msg []byte) {
		go func() {
			connInfo := &ConnzOptions{}
			conns, _ := s.Connz(connInfo)
			for _, conn := range conns.Conns {
				connId := strings.Split(conn.Name, "::")[0]
				message := strings.TrimSuffix(string(msg), "\r\n")
				if connId == message {
					s.sendInternalAccountMsgWithReply(s.GlobalAccount(), reply, _EMPTY_, nil, []byte("connExists"), true)
					return
				}
			}
		}()
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) KillZombieResources() {
	respCh := make(chan []byte)

	for range time.Tick(time.Second * 30) {
		s.Debugf("Killing Zombie resources iteration")
		var zombieConnections []primitive.ObjectID
		connections, err := getActiveConnections()
		if err != nil {
			serv.Errorf("KillZombieResources error: " + err.Error())
			continue
		}

		for _, conn := range connections {
			msg := (conn.ID).Hex()
			reply := CONN_STATUS_SUBJ + "_reply" + s.memphis.nuid.Next()

			sub, err := s.subscribeOnGlobalAcc(reply, reply+"_sid", func(_ *client, subject, reply string, msg []byte) {
				go func() { respCh <- msg }()
			})
			if err != nil {
				serv.Errorf("KillZombieResources error: " + err.Error())
				continue
			}

			s.sendInternalAccountMsgWithReply(s.GlobalAccount(), CONN_STATUS_SUBJ, reply, nil, msg, true)
			timeout := time.After(10 * time.Second)
			select {
			case <-respCh:
				continue
			case <-timeout:
				zombieConnections = append(zombieConnections, conn.ID)
			}
			sub.close()
		}

		if len(zombieConnections) > 0 {
			serv.Warnf("Zombie connection found, killing")
			err := killRelevantConnections(zombieConnections)
			if err != nil {
				serv.Errorf("KillZombieResources error: " + err.Error())
			} else {
				err = killProducersByConnections(zombieConnections)
				if err != nil {
					serv.Errorf("KillZombieResources error: " + err.Error())
				}

				err = killConsumersByConnections(zombieConnections)
				if err != nil {
					serv.Errorf("KillZombieResources error: " + err.Error())
				}
			}
		}

		err = removeOldPoisonMsgs()
		if err != nil {
			serv.Errorf("KillZombieResources error: " + err.Error())
		}

		if err = s.removeRedundantStations(); err != nil {
			serv.Errorf("KillZombieResources error: " + err.Error())
		}
	}
}
