package shared

import (
	"encoding/json"
	"log"
	"sync"

	"video-streaming-server/types"
)

// map of user ID and SessionSSEChannelMap
var GlobalUserSSEConnectionsMap = make(map[types.UserID]types.SessionSSEChannelMap)
var SSEConnectionsMutex sync.RWMutex

func SendEventToUser(userID types.UserID, event string, data any) {
	_ = SendEventToUserWithID(userID, event, "", data)
}

func SendEventToUserByID(userID string, event string, eventID string, data any) bool {
	return SendEventToUserWithID(types.UserID(userID), event, eventID, data)
}

func SendEventToUserWithID(userID types.UserID, event string, eventID string, data any) bool {
	SSEConnectionsMutex.RLock()
	defer SSEConnectionsMutex.RUnlock()
	userSSEChannelMap, userExists := GlobalUserSSEConnectionsMap[userID]
	if !userExists {
		log.Printf("user %s not found in GlobalUserSSEConnectionsMap", userID)
		return true
	}
	userSessions := userSSEChannelMap.Sessions
	accepted := true
	for sessionID, channel := range userSessions {
		eventData, err := json.Marshal(data)
		if err != nil {
			log.Printf("failed marshalling data for event %s to user %s on session %s", event, userID, sessionID)
			return true
		}
		log.Printf("sending event %s data %s to user %s on session %s", event, eventData, userID, sessionID)

		message := types.SSEType{
			ID:    eventID,
			Event: event,
			Data:  data,
		}
		select {
		case channel.EventChannel <- message:
		default:
			log.Printf("dropping event %s for user %s on session %s because the SSE channel is full", event, userID, sessionID)
			accepted = false
		}
	}
	return accepted
}

func GetSSEChannel(userID types.UserID, sessionID types.SessionID) (types.SSEChannel, bool) {
	SSEConnectionsMutex.RLock()
	defer SSEConnectionsMutex.RUnlock()
	userChannels, exists := GlobalUserSSEConnectionsMap[userID]
	if !exists {
		return types.SSEChannel{}, false
	}
	channel, exists := userChannels.Sessions[sessionID]
	return channel, exists
}
