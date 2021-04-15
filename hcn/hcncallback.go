package hcn

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Microsoft/hcsshim/internal/interop"
)

var (
	nextCallback    uintptr
	callbackMap     = map[uintptr]*notifcationWatcherContext{}
	callbackMapLock = sync.RWMutex{}

	notificationWatcherCallback = syscall.NewCallback(notificationWatcher)

	// Notifications for HCN_SERVICE handles
	HcnNotificationNetworkPreCreate HcnNotification = 0x00000001
	HcnNotificationNetworkCreate    HcnNotification = 0x00000002
	HcnNotificationNetworkPreDelete HcnNotification = 0x00000003
	HcnNotificationNetworkDelete    HcnNotification = 0x00000004

	// Namespace Notifications
	HcnNotificationNamespaceCreate HcnNotification = 0x00000005
	HcnNotificationNamespaceDelete HcnNotification = 0x00000006

	// Notifications for HCN_SERVICE handles
	HcnNotificationGuestNetworkServiceCreate HcnNotification = 0x00000007
	HcnNotificationGuestNetworkServiceDelete HcnNotification = 0x00000008

	// Notifications for HCN_NETWORK handles
	HcnNotificationNetworkEndpointAttached HcnNotification = 0x00000009
	HcnNotificationNetworkEndpointDetached HcnNotification = 0x00000010

	// Notifications for HCN_GUESTNETWORKSERVICE handles
	HcnNotificationGuestNetworkServiceStateChanged          HcnNotification = 0x00000011
	HcnNotificationGuestNetworkServiceInterfaceStateChanged HcnNotification = 0x00000012

	// Common notifications
	HcnNotificationServiceDisconnect HcnNotification = 0x01000000
)

type HcnNotification uint32

func (hn HcnNotification) String() string {
	switch hn {
	case HcnNotificationNetworkPreCreate:
		return "NetworkPreCreate"
	case HcnNotificationNetworkCreate:
		return "NetworkCreate"
	case HcnNotificationNetworkPreDelete:
		return "NetworkPreDelete"
	case HcnNotificationNetworkDelete:
		return "NetworkDelete"
	case HcnNotificationNamespaceCreate:
		return "NamespaceCreate"
	case HcnNotificationNamespaceDelete:
		return "NamespaceDelete"
	case HcnNotificationGuestNetworkServiceCreate:
		return "GuestNetworkServiceCreate"
	case HcnNotificationGuestNetworkServiceDelete:
		return "GuestNetworkServiceDelete"
	case HcnNotificationNetworkEndpointAttached:
		return "NetworkEndpointAttached"
	case HcnNotificationNetworkEndpointDetached:
		return "NetworkEndpointDetached"
	case HcnNotificationGuestNetworkServiceStateChanged:
		return "GuestNetworkServiceStateChanged"
	case HcnNotificationGuestNetworkServiceInterfaceStateChanged:
		return "GuestNetworkServiceInterfaceStateChanged"
	case HcnNotificationServiceDisconnect:
		return "ServiceDisconnect"
	default:
		return fmt.Sprintf("Unknown: %d", hn)
	}
}

type HcnNotificationData struct {
	Type   HcnNotification
	Status error
	Data   NotificationBase
}

type notificationChannel chan HcnNotificationData

type notifcationWatcherContext struct {
	channel notificationChannel
	handle  hcnCallbackHandle
}

func notificationWatcher(notificationType HcnNotification, callbackNumber uintptr, notificationStatus uintptr, notificationData *uint16) uintptr {
	var result error
	if int32(notificationStatus) < 0 {
		result = interop.Win32FromHresult(notificationStatus)
	}

	callbackMapLock.RLock()
	context := callbackMap[callbackNumber]
	callbackMapLock.RUnlock()

	if context == nil {
		return 0
	}

	tmpString := syscall.UTF16ToString((*[1 << 29]uint16)(unsafe.Pointer(notificationData))[:])

	var notifData NotificationBase
	err := json.Unmarshal([]byte(tmpString), &notifData)
	if err == nil {
		context.channel <- HcnNotificationData{
			Type:   notificationType,
			Status: result,
			Data:   notifData,
		}
	}

	return 0
}
