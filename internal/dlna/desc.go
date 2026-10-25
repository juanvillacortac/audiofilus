package dlna

import (
	"fmt"
	"strings"
)

// Los tipos de dispositivo y de servicio de UPnP AV.
const (
	typeServer   = "urn:schemas-upnp-org:device:MediaServer:1"
	typeRenderer = "urn:schemas-upnp-org:device:MediaRenderer:1"
	svcCD        = "urn:schemas-upnp-org:service:ContentDirectory:1"
	svcCM        = "urn:schemas-upnp-org:service:ConnectionManager:1"
	svcAVT       = "urn:schemas-upnp-org:service:AVTransport:1"
	svcRC        = "urn:schemas-upnp-org:service:RenderingControl:1"
)

// service es un servicio de un dispositivo: su tipo, su id y sus rutas (bajo
// el prefijo del dispositivo).
type service struct {
	typ, id, name string
}

// deviceDesc es la descripción de un dispositivo (la que pide un cliente
// tras verlo por SSDP), con sus servicios en /<prefijo>/<nombre>/.
func deviceDesc(devType, name, uuid, prefix string, services []service) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<device>
`)
	fmt.Fprintf(&b, "<deviceType>%s</deviceType>\n", devType)
	fmt.Fprintf(&b, "<friendlyName>%s</friendlyName>\n", xmlEscape(name))
	b.WriteString(`<manufacturer>Audiofilus</manufacturer>
<manufacturerURL>https://github.com/juanvillacortac/audiofilus</manufacturerURL>
<modelDescription>audiofilus music player</modelDescription>
<modelName>Audiofilus</modelName>
<modelNumber>1</modelNumber>
<modelURL>https://github.com/juanvillacortac/audiofilus</modelURL>
`)
	if devType == typeServer {
		b.WriteString("<dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>\n")
	} else {
		b.WriteString("<dlna:X_DLNADOC>DMR-1.50</dlna:X_DLNADOC>\n")
	}
	fmt.Fprintf(&b, "<UDN>uuid:%s</UDN>\n", uuid)
	fmt.Fprintf(&b, `<iconList><icon><mimetype>image/png</mimetype><width>48</width><height>48</height><depth>24</depth><url>/%s/icon.png</url></icon></iconList>
`, prefix)
	b.WriteString("<serviceList>\n")
	for _, s := range services {
		fmt.Fprintf(&b, `<service><serviceType>%s</serviceType><serviceId>%s</serviceId><SCPDURL>/%s/%s/scpd.xml</SCPDURL><controlURL>/%s/%s/control</controlURL><eventSubURL>/%s/%s/event</eventSubURL></service>
`, s.typ, s.id, prefix, s.name, prefix, s.name, prefix, s.name)
	}
	b.WriteString("</serviceList>\n</device>\n</root>\n")
	return b.String()
}

// Los servicios de cada dispositivo.
var (
	serverServices = []service{
		{svcCD, "urn:upnp-org:serviceId:ContentDirectory", "cd"},
		{svcCM, "urn:upnp-org:serviceId:ConnectionManager", "cm"},
	}
	rendererServices = []service{
		{svcAVT, "urn:upnp-org:serviceId:AVTransport", "avt"},
		{svcRC, "urn:upnp-org:serviceId:RenderingControl", "rc"},
		{svcCM, "urn:upnp-org:serviceId:ConnectionManager", "cm"},
	}
)

// scpdAction es una acción en una descripción de servicio: sus argumentos
// de entrada y de salida, cada uno con su variable de estado.
type scpdAction struct {
	name    string
	in, out [][2]string // nombre, variable
}

// stateVar es una variable de estado: nombre, tipo, si manda eventos y los
// valores permitidos.
type stateVar struct {
	name, typ string
	events    bool
	allowed   []string
}

// scpd escribe la descripción de un servicio.
func scpd(actions []scpdAction, vars []stateVar) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<actionList>
`)
	for _, a := range actions {
		fmt.Fprintf(&b, "<action><name>%s</name><argumentList>", a.name)
		for _, x := range a.in {
			fmt.Fprintf(&b, "<argument><name>%s</name><direction>in</direction><relatedStateVariable>%s</relatedStateVariable></argument>", x[0], x[1])
		}
		for _, x := range a.out {
			fmt.Fprintf(&b, "<argument><name>%s</name><direction>out</direction><relatedStateVariable>%s</relatedStateVariable></argument>", x[0], x[1])
		}
		b.WriteString("</argumentList></action>\n")
	}
	b.WriteString("</actionList>\n<serviceStateTable>\n")
	for _, v := range vars {
		ev := "no"
		if v.events {
			ev = "yes"
		}
		fmt.Fprintf(&b, `<stateVariable sendEvents="%s"><name>%s</name><dataType>%s</dataType>`, ev, v.name, v.typ)
		if len(v.allowed) > 0 {
			b.WriteString("<allowedValueList>")
			for _, a := range v.allowed {
				fmt.Fprintf(&b, "<allowedValue>%s</allowedValue>", a)
			}
			b.WriteString("</allowedValueList>")
		}
		b.WriteString("</stateVariable>\n")
	}
	b.WriteString("</serviceStateTable>\n</scpd>\n")
	return b.String()
}

// Las descripciones de los servicios.
var (
	scpdCD = scpd([]scpdAction{
		{"GetSearchCapabilities", nil, [][2]string{{"SearchCaps", "SearchCapabilities"}}},
		{"GetSortCapabilities", nil, [][2]string{{"SortCaps", "SortCapabilities"}}},
		{"GetSystemUpdateID", nil, [][2]string{{"Id", "SystemUpdateID"}}},
		{"Browse", [][2]string{{"ObjectID", "A_ARG_TYPE_ObjectID"}, {"BrowseFlag", "A_ARG_TYPE_BrowseFlag"}, {"Filter", "A_ARG_TYPE_Filter"}, {"StartingIndex", "A_ARG_TYPE_Index"}, {"RequestedCount", "A_ARG_TYPE_Count"}, {"SortCriteria", "A_ARG_TYPE_SortCriteria"}},
			[][2]string{{"Result", "A_ARG_TYPE_Result"}, {"NumberReturned", "A_ARG_TYPE_Count"}, {"TotalMatches", "A_ARG_TYPE_Count"}, {"UpdateID", "A_ARG_TYPE_UpdateID"}}},
		{"Search", [][2]string{{"ContainerID", "A_ARG_TYPE_ObjectID"}, {"SearchCriteria", "A_ARG_TYPE_SearchCriteria"}, {"Filter", "A_ARG_TYPE_Filter"}, {"StartingIndex", "A_ARG_TYPE_Index"}, {"RequestedCount", "A_ARG_TYPE_Count"}, {"SortCriteria", "A_ARG_TYPE_SortCriteria"}},
			[][2]string{{"Result", "A_ARG_TYPE_Result"}, {"NumberReturned", "A_ARG_TYPE_Count"}, {"TotalMatches", "A_ARG_TYPE_Count"}, {"UpdateID", "A_ARG_TYPE_UpdateID"}}},
	}, []stateVar{
		{"SearchCapabilities", "string", false, nil},
		{"SortCapabilities", "string", false, nil},
		{"SystemUpdateID", "ui4", true, nil},
		{"A_ARG_TYPE_ObjectID", "string", false, nil},
		{"A_ARG_TYPE_Result", "string", false, nil},
		{"A_ARG_TYPE_SearchCriteria", "string", false, nil},
		{"A_ARG_TYPE_BrowseFlag", "string", false, []string{"BrowseMetadata", "BrowseDirectChildren"}},
		{"A_ARG_TYPE_Filter", "string", false, nil},
		{"A_ARG_TYPE_SortCriteria", "string", false, nil},
		{"A_ARG_TYPE_Index", "ui4", false, nil},
		{"A_ARG_TYPE_Count", "ui4", false, nil},
		{"A_ARG_TYPE_UpdateID", "ui4", false, nil},
	})

	scpdCM = scpd([]scpdAction{
		{"GetProtocolInfo", nil, [][2]string{{"Source", "SourceProtocolInfo"}, {"Sink", "SinkProtocolInfo"}}},
		{"GetCurrentConnectionIDs", nil, [][2]string{{"ConnectionIDs", "CurrentConnectionIDs"}}},
		{"GetCurrentConnectionInfo", [][2]string{{"ConnectionID", "A_ARG_TYPE_ConnectionID"}},
			[][2]string{{"RcsID", "A_ARG_TYPE_RcsID"}, {"AVTransportID", "A_ARG_TYPE_AVTransportID"}, {"ProtocolInfo", "A_ARG_TYPE_ProtocolInfo"}, {"PeerConnectionManager", "A_ARG_TYPE_ConnectionManager"}, {"PeerConnectionID", "A_ARG_TYPE_ConnectionID"}, {"Direction", "A_ARG_TYPE_Direction"}, {"Status", "A_ARG_TYPE_ConnectionStatus"}}},
	}, []stateVar{
		{"SourceProtocolInfo", "string", true, nil},
		{"SinkProtocolInfo", "string", true, nil},
		{"CurrentConnectionIDs", "string", true, nil},
		{"A_ARG_TYPE_ConnectionStatus", "string", false, []string{"OK", "ContentFormatMismatch", "InsufficientBandwidth", "UnreliableChannel", "Unknown"}},
		{"A_ARG_TYPE_ConnectionManager", "string", false, nil},
		{"A_ARG_TYPE_Direction", "string", false, []string{"Input", "Output"}},
		{"A_ARG_TYPE_ProtocolInfo", "string", false, nil},
		{"A_ARG_TYPE_ConnectionID", "i4", false, nil},
		{"A_ARG_TYPE_AVTransportID", "i4", false, nil},
		{"A_ARG_TYPE_RcsID", "i4", false, nil},
	})

	scpdAVT = scpd([]scpdAction{
		{"SetAVTransportURI", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"CurrentURI", "AVTransportURI"}, {"CurrentURIMetaData", "AVTransportURIMetaData"}}, nil},
		{"SetNextAVTransportURI", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"NextURI", "NextAVTransportURI"}, {"NextURIMetaData", "NextAVTransportURIMetaData"}}, nil},
		{"GetMediaInfo", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}},
			[][2]string{{"NrTracks", "NumberOfTracks"}, {"MediaDuration", "CurrentMediaDuration"}, {"CurrentURI", "AVTransportURI"}, {"CurrentURIMetaData", "AVTransportURIMetaData"}, {"NextURI", "NextAVTransportURI"}, {"NextURIMetaData", "NextAVTransportURIMetaData"}, {"PlayMedium", "PlaybackStorageMedium"}, {"RecordMedium", "RecordStorageMedium"}, {"WriteStatus", "RecordMediumWriteStatus"}}},
		{"GetTransportInfo", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}},
			[][2]string{{"CurrentTransportState", "TransportState"}, {"CurrentTransportStatus", "TransportStatus"}, {"CurrentSpeed", "TransportPlaySpeed"}}},
		{"GetPositionInfo", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}},
			[][2]string{{"Track", "CurrentTrack"}, {"TrackDuration", "CurrentTrackDuration"}, {"TrackMetaData", "CurrentTrackMetaData"}, {"TrackURI", "CurrentTrackURI"}, {"RelTime", "RelativeTimePosition"}, {"AbsTime", "AbsoluteTimePosition"}, {"RelCount", "RelativeCounterPosition"}, {"AbsCount", "AbsoluteCounterPosition"}}},
		{"GetDeviceCapabilities", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}},
			[][2]string{{"PlayMedia", "PossiblePlaybackStorageMedia"}, {"RecMedia", "PossibleRecordStorageMedia"}, {"RecQualityModes", "PossibleRecordQualityModes"}}},
		{"GetTransportSettings", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}},
			[][2]string{{"PlayMode", "CurrentPlayMode"}, {"RecQualityMode", "CurrentRecordQualityMode"}}},
		{"Stop", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, nil},
		{"Play", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Speed", "TransportPlaySpeed"}}, nil},
		{"Pause", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, nil},
		{"Seek", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Unit", "A_ARG_TYPE_SeekMode"}, {"Target", "A_ARG_TYPE_SeekTarget"}}, nil},
		{"Next", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, nil},
		{"Previous", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, nil},
		{"GetCurrentTransportActions", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, [][2]string{{"Actions", "CurrentTransportActions"}}},
	}, []stateVar{
		{"TransportState", "string", false, []string{"STOPPED", "PLAYING", "PAUSED_PLAYBACK", "TRANSITIONING", "NO_MEDIA_PRESENT"}},
		{"TransportStatus", "string", false, []string{"OK", "ERROR_OCCURRED"}},
		{"PlaybackStorageMedium", "string", false, nil},
		{"RecordStorageMedium", "string", false, nil},
		{"PossiblePlaybackStorageMedia", "string", false, nil},
		{"PossibleRecordStorageMedia", "string", false, nil},
		{"CurrentPlayMode", "string", false, []string{"NORMAL"}},
		{"TransportPlaySpeed", "string", false, nil},
		{"RecordMediumWriteStatus", "string", false, nil},
		{"CurrentRecordQualityMode", "string", false, nil},
		{"PossibleRecordQualityModes", "string", false, nil},
		{"NumberOfTracks", "ui4", false, nil},
		{"CurrentTrack", "ui4", false, nil},
		{"CurrentTrackDuration", "string", false, nil},
		{"CurrentMediaDuration", "string", false, nil},
		{"CurrentTrackMetaData", "string", false, nil},
		{"CurrentTrackURI", "string", false, nil},
		{"AVTransportURI", "string", false, nil},
		{"AVTransportURIMetaData", "string", false, nil},
		{"NextAVTransportURI", "string", false, nil},
		{"NextAVTransportURIMetaData", "string", false, nil},
		{"RelativeTimePosition", "string", false, nil},
		{"AbsoluteTimePosition", "string", false, nil},
		{"RelativeCounterPosition", "i4", false, nil},
		{"AbsoluteCounterPosition", "i4", false, nil},
		{"CurrentTransportActions", "string", false, nil},
		{"LastChange", "string", true, nil},
		{"A_ARG_TYPE_SeekMode", "string", false, []string{"REL_TIME", "ABS_TIME", "TRACK_NR"}},
		{"A_ARG_TYPE_SeekTarget", "string", false, nil},
		{"A_ARG_TYPE_InstanceID", "ui4", false, nil},
	})

	scpdRC = scpd([]scpdAction{
		{"ListPresets", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}}, [][2]string{{"CurrentPresetNameList", "PresetNameList"}}},
		{"SelectPreset", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"PresetName", "A_ARG_TYPE_PresetName"}}, nil},
		{"GetVolume", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Channel", "A_ARG_TYPE_Channel"}}, [][2]string{{"CurrentVolume", "Volume"}}},
		{"SetVolume", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Channel", "A_ARG_TYPE_Channel"}, {"DesiredVolume", "Volume"}}, nil},
		{"GetMute", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Channel", "A_ARG_TYPE_Channel"}}, [][2]string{{"CurrentMute", "Mute"}}},
		{"SetMute", [][2]string{{"InstanceID", "A_ARG_TYPE_InstanceID"}, {"Channel", "A_ARG_TYPE_Channel"}, {"DesiredMute", "Mute"}}, nil},
	}, []stateVar{
		{"PresetNameList", "string", false, nil},
		{"Volume", "ui2", false, nil},
		{"Mute", "boolean", false, nil},
		{"LastChange", "string", true, nil},
		{"A_ARG_TYPE_Channel", "string", false, []string{"Master"}},
		{"A_ARG_TYPE_InstanceID", "ui4", false, nil},
		{"A_ARG_TYPE_PresetName", "string", false, []string{"FactoryDefaults"}},
	})
)
