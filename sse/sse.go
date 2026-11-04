package sse

import (
	"log"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/types"
	"video-streaming-server/utils"

	"github.com/google/uuid"
)

func CreateNewSSEChannel(refererPath string, bufferSize int) types.SSEChannel {
	return types.SSEChannel{
		OriginatingPage: refererPath,
		EventChannel:    make(chan types.SSEType, bufferSize),
	}
}

func InitializeSSEConnection(userID types.UserID, path string) types.SessionID {
	sessionID := types.SessionID(uuid.New().String())
	shared.SSEConnectionsMutex.Lock()
	defer shared.SSEConnectionsMutex.Unlock()
	userSSEChannelMap, userExists := shared.GlobalUserSSEConnectionsMap[userID]
	if !userExists {
		logger.Log.Debug("user not found in GlobalUserSSEConnectionsMap, creating new entry", "userID", userID)
		userSSEChannelMap = types.SessionSSEChannelMap{
			Sessions: make(map[types.SessionID]types.SSEChannel),
		}
	}
	userSSEChannelMap.Sessions[sessionID] = CreateNewSSEChannel(path, 10)
	shared.GlobalUserSSEConnectionsMap[userID] = userSSEChannelMap
	return sessionID
}

func RemoveSSEConnection(userID types.UserID, sessionID types.SessionID) {
	shared.SSEConnectionsMutex.Lock()
	defer shared.SSEConnectionsMutex.Unlock()
	userSSEChannelMap, userExists := shared.GlobalUserSSEConnectionsMap[userID]
	if !userExists {
		logger.Log.Error("user not found in GlobalUserSSEConnectionsMap", "userID", userID)
		return
	}
	channel, channelExists := userSSEChannelMap.Sessions[sessionID]
	if !channelExists {
		logger.Log.Error("session not found in user's SSE channel map", "userID", userID, "sessionID", sessionID)
		return
	}
	log.Printf("closing channel for user %s, session %s", userID, sessionID)
	close(channel.EventChannel)
	logger.Log.Debug("channel closed for user", "userID", userID, "sessionID", sessionID)
	delete(userSSEChannelMap.Sessions, sessionID)
	if len(userSSEChannelMap.Sessions) == 0 {
		logger.Log.Debug("no more sessions for user, removing from GlobalUserSSEConnectionsMap", "userID", userID)
		delete(shared.GlobalUserSSEConnectionsMap, userID)
	} else {
		shared.GlobalUserSSEConnectionsMap[userID] = userSSEChannelMap // clarity
	}

	utils.PrettyPrintMap(shared.GlobalUserSSEConnectionsMap, "GlobalUserSSEConnectionsMap")
}
