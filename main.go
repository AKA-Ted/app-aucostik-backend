package main

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	deviceName    = "Micrófono externo"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	clients   = make(map[*websocket.Conn]bool)
	audioChan = make(chan []byte)
)

// FindDeviceIndex busca un dispositivo de audio por nombre y devuelve su índice.
func FindDeviceIndex(deviceName string) (int, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return -1, errors.New("error obteniendo los dispositivos")
	}

	for _, device := range devices {
		if device.Name == deviceName {
			return device.Index, nil
		}
	}

	// Si no lo encuentra, devuelve un error.
	return -1, errors.New("dispositivo no encontrado")
}

func Int16toArrayBytes(buffer []int16) ([]byte, error) {
	byteBuffer := new(bytes.Buffer)
	err := binary.Write(byteBuffer, binary.LittleEndian, buffer)
	if err != nil {
		log.Println("Error converting int16 to bytes:", err)
	}
	return byteBuffer.Bytes(), nil
}

// Hilo 1: Captura audio continuamente y lo envía al canal `audioChan`
func captureAudio() {

	// Buscar el índice del dispositivo "Micrófono externo"
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

	// Se crea el buffer de entrada
	buffer := make([]int16, bufferSize)

	// Abrir stream de audio usando la función del paquete util
	stream, err := portaudio.OpenDefaultStream(channel, 0, float64(sampleRate), bufferSize, buffer)
	if err != nil {
		log.Fatal("Error abriendo el stream:", err)
	}
	defer stream.Close()

	err = stream.Start()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Capturando audio...")

	// Capturar audio en un bucle infinito
	for {
		// Leer datos del stream de audio
		err := stream.Read()
		if err != nil {
			log.Fatalf("Error leyendo del stream: %v", err)
			continue
		}

		// Verificar los primeros 5 valores del buffer
		// log.Printf("🎙️ Muestras de audio int16: %v\n", buffer[:5])

		audioData, err := Int16toArrayBytes(buffer)
		if err != nil {
			log.Println("Error convirtiendo buffer:", err)
			continue
		}

		// Verificar si el audioData convertido tiene datos
		log.Printf("📦 Datos binarios (primeros 5 bytes): %v\n", audioData[:5])
		// 1024 muestras * 1 canal * 2 bytes/muestra = 2048 bytes
		audioChan <- audioData
	}
}

// Hilo 2: Escucha `audioChan` y reenvía los datos por WebSocket
func broadcastAudio() {
	for audioData := range audioChan {
		// Verificar si el buffer recibido tiene datos
		// if len(audioData) > 0 {
		// 	log.Printf("📤 Enviando audio (%d bytes). Muestra: %v", len(audioData), audioData[:10])
		// } else {
		// 	log.Println("⚠️ Se intentó enviar un buffer vacío")
		// }

		// Enviar audio a los clientes conectados
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
