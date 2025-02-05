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
package routes

import (
	"memphis-broker/server"

	"github.com/gin-gonic/gin"
)

func InitializeStationsRoutes(router *gin.RouterGroup, h *server.Handlers) {
	stationsHandler := h.Stations
	stationsRoutes := router.Group("/stations")
	stationsRoutes.GET("/getStation", stationsHandler.GetStation)
	stationsRoutes.GET("/getMessageDetails", stationsHandler.GetMessageDetails)
	stationsRoutes.GET("/getAllStations", stationsHandler.GetAllStations)
	stationsRoutes.GET("/getStations", stationsHandler.GetStations)
	stationsRoutes.GET("/getPoisonMessageJourney", stationsHandler.GetPoisonMessageJourney)
	stationsRoutes.POST("/createStation", stationsHandler.CreateStation)
	stationsRoutes.POST("/resendPoisonMessages", stationsHandler.ResendPoisonMessages)
	stationsRoutes.POST("/ackPoisonMessages", stationsHandler.AckPoisonMessages)
	stationsRoutes.DELETE("/removeStation", stationsHandler.RemoveStation)
}
