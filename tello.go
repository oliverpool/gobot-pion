package main

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"gobot.io/x/gobot"
	"gobot.io/x/gobot/platforms/dji/tello"
)

type Tello struct {
	*tello.Driver
	flightdata chan FlightData
	videoTrack *webrtc.TrackLocalStaticSample
}

func NewTello() (Tello, error) {
	drone := tello.NewDriver("8890")

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"tello",
	)
	if err != nil {
		return Tello{}, err
	}
	t := Tello{
		Driver:     drone,
		flightdata: make(chan FlightData),
		videoTrack: videoTrack,
	}

	robot := gobot.NewRobot("tello",
		[]gobot.Connection{},
		[]gobot.Device{drone},
		t.startVideo,
	)

	return t, robot.Start(false)
}

func (t Tello) VideoTrack() webrtc.TrackLocal {
	return t.videoTrack
}

func (t Tello) FlightData() <-chan FlightData {
	return t.flightdata
}

func (t Tello) startVideo() {
	drone := t.Driver

	drone.On(tello.ConnectedEvent, func(data interface{}) {
		fmt.Println("Connected")
		drone.StartVideo()
		drone.SetVideoEncoderRate(tello.VideoBitRateAuto)
		gobot.Every(100*time.Millisecond, func() {
			drone.StartVideo()
		})
	})

	// We buffer the bytes, until it looks like a good h264 frame
	//
	// Thanks to https://yumichan.net/video-processing/video-compression/introduction-to-h264-nal-unit/
	var buf []byte
	isNalUnitStart := func(b []byte) bool {
		return len(b) > 3 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1
	}
	sendPreviousBytes := func(b []byte) bool {
		// Tello sends NAL units of type: 1 1 7 8 5 (and so on)
		// We don't want to send NAL 7 or 8 alone (they don't have frame info)
		// We only send when the next NAL unit is 1 or 7: [1], [1], [785]
		return len(b) > 4 && (b[4]&0b11111 == 7 || b[4]&0b11111 == 1)
	}

	drone.On(tello.VideoFrameEvent, func(data interface{}) {
		b := data.([]byte)
		if len(buf) > 0 && isNalUnitStart(b) && sendPreviousBytes(b) {
			t.videoTrack.WriteSample(media.Sample{
				Data:     buf,
				Duration: 5 * time.Millisecond, // just a wild guess
			})
			buf = b
		} else {
			buf = append(buf, b...)
		}
	})

	var previousFlightData FlightData
	drone.On(tello.FlightDataEvent, func(data interface{}) {
		allData := data.(*tello.FlightData)
		flightData := FlightData{
			Height:            int(allData.Height),
			BatteryPercentage: int(allData.BatteryPercentage),
		}
		if previousFlightData != flightData {
			t.flightdata <- flightData
			previousFlightData = flightData
		}

	})
}
