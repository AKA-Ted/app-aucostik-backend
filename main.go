package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"

	"github.com/gordonklaus/portaudio"
	"github.com/gorilla/websocket"
)

const (
	sampleRate    = 48000 // Tasa de muestreo 48KHz
	bufferSize    = 1024  // Tamaño del buffer
	channel       = 1     // Número de canales
	bitsPerSample = 16    // 16 bits por muestra
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	clients   = make(map[*websocket.Conn]bool)
	audioChan = make(chan []byte)
)

// Struct para los parametros del Device
// type StreamDeviceParameters struct {
// 	Device   *portaudio.DeviceInfo // Dispositivo de audio
// 	Channels int                   // Número de canales
// }

// Struct para los parametros del Stream
type StreamParameters struct {
	NumInputChannels  int // Número de canales de entrada
	NumOutputChannels int // Número de canales de salida
	SampleRate        int // Tasa de muestreo
	BufferFrames      int // Tamaño del buffer
	ProcessAudio      int //processAudio,         // Callback para procesar el audio
}

func OpenAudioStream(params StreamParameters) (*portaudio.Stream, error) {
	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		SampleRate:      float64(params.SampleRate),
		FramesPerBuffer: params.BufferFrames,
	})

	if err != nil {
		return nil, fmt.Errorf("error abriendo stream de audio: %v", err)
	}

	return stream, nil
}

// FindDeviceIndex busca un dispositivo de audio por nombre y devuelve su índice.
func FindDeviceIndex(deviceName string) (int, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return -1, fmt.Errorf("error obteniendo los dispositivos: %v", err)
	}

	for _, device := range devices {
		if device.Name == deviceName {
			return device.Index, nil
		}
	}

	// Si no lo encuentra, devuelve un error.
	return -1, fmt.Errorf("dispositivo no encontrado: %s", deviceName)
}

// Float32ToBytes convierte un slice de float32 (muestras de audio) en un slice de bytes.
func Float32ToBytes(samples []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	for _, sample := range samples {
		if err := binary.Write(buf, binary.LittleEndian, sample); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// Hilo 1: Captura audio continuamente y lo envía al canal `audioChan`
func captureAudio() {
	// buffer := make([]int16, bufferSize)

	// Buscar el índice del dispositivo "Micrófono externo"
	deviceName := "Micrófono externo"
	deviceIndex, err := FindDeviceIndex(deviceName)

	if err != nil {
		log.Fatalf("Error buscando el dispositivo: %v", err)
	}

	// Obtener el dispositivo de entrada
	devices, err := portaudio.Devices()
	if err != nil {
		log.Fatal("Error obteniendo los dispositivos:", err)
	}

	defaultDevice := devices[deviceIndex]

	log.Printf("Usando dispositivo: %s (Index: %d, Canales: %d, Canales de salida: %d, SampleRate: %f)",
		defaultDevice.Name, defaultDevice.Index, defaultDevice.MaxInputChannels, defaultDevice.MaxOutputChannels, defaultDevice.DefaultSampleRate)

	// Configurar los parámetros del dispositivo
	// deviceParams := StreamDeviceParameters{
	// 	Device:   defaultDevice,
	// 	Channels: channel,
	// }

	// Configurar los parámetros del stream
	streamParams := StreamParameters{
		NumInputChannels:  channel,    // Número de canales de entrada
		NumOutputChannels: 0,          // Número de canales de salida
		SampleRate:        sampleRate, // Tasa de muestreo
		BufferFrames:      bufferSize, // Tamaño del buffer
	}

	// Abrir stream de audio usando la función del paquete util
	stream, err := OpenAudioStream(streamParams)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		log.Fatal("Error iniciando el stream:", err)
	}
	defer stream.Stop()

	log.Println("Capturando audio...")

	// Capturar audio en un bucle infinito
	for {
		// Leer datos del stream de audio
		err = stream.Read()

		// Error handling
		if err != nil {
			log.Fatalf("Error leyendo del stream: %v", err)
			continue
		}

		// byteArray, err := Float32ToBytes(buffer)

		if err != nil {
			log.Println("Error convirtiendo datos a bytes:", err)
			continue
		}
		// 1024 muestras * 1 canal * 4 bytes/muestra = 4096 bytes
		// audioChan <- byteArray
	}
}

// Hilo 2: Escucha `audioChan` y reenvía los datos por WebSocket
func broadcastAudio() {
	for audioData := range audioChan {
		for client := range clients {
			err := client.WriteMessage(websocket.BinaryMessage, audioData)
			if err != nil {
				log.Println("Error enviando datos al cliente, cerrando conexión:", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}

// Maneja conexiones WebSocket
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error en WebSocket:", err)
		return
	}

	clients[conn] = true
	log.Println("Nuevo cliente conectado")

	// Mantener la conexión abierta hasta que se cierre
	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}

	delete(clients, conn)
	conn.Close()
}

func main() {
	// Inicializar PortAudio
	if err := portaudio.Initialize(); err != nil {
		log.Fatal("Error inicializando PortAudio:", err)
	}
	defer portaudio.Terminate()

	// Iniciar hilos (goroutines)
	go captureAudio()   // Hilo que captura audio
	go broadcastAudio() // Hilo que envía audio

	// Configurar WebSocket
	http.HandleFunc("/channel", wsHandler)
	log.Println("Servidor WebSocket en http://127.0.0.1:5555/channel")
	log.Fatal(http.ListenAndServe(":5555", nil))
}
