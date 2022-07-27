package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Local                       Mode   = "local"
	Remote                             = "remote"
	ConfigDefaultName           string = "config.json"
	ConfigDefaultListenPort     int    = 8080
	ConfigDefaultFolder                = "cache"
	ConfigDefaultUpdateInterval        = 10
	ConfigDefaultMaxCacheSize          = 100
	ImageCompressQuality               = 60
)

type Mode string

type Config struct {
	ListenPort     int
	Mode           Mode
	CacheFolder    string
	UpdateInterval int64
	MaxCacheSize   int
	Remotes        []string
}

func readConfig() Config {
	// Read config file
	file, err := ioutil.ReadFile(ConfigDefaultName)
	if err != nil {
		log.Fatalln("error:", err)
	}
	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		log.Fatalln("error:", err)
	}
	return config
}

func writeConfig(config Config) {
	// Write config struct to json file
	file, _ := json.Marshal(config)
	err := ioutil.WriteFile(ConfigDefaultName, file, 0644)
	if err != nil {
		fmt.Println("error:", err)
	}
}

func getConfig() Config {
	// Reacd/Write/Create config file
	var config Config
	if _, err := os.Stat(ConfigDefaultName); err == nil {
		config = readConfig()
	} else if errors.Is(err, os.ErrNotExist) {
		// No config file, create one with default values
		config = Config{
			ListenPort:     ConfigDefaultListenPort,
			Mode:           Remote,
			CacheFolder:    ConfigDefaultFolder,
			UpdateInterval: ConfigDefaultUpdateInterval,
			MaxCacheSize:   ConfigDefaultMaxCacheSize,
			Remotes:        []string{"https://www.loliapi.com/acg", "https://api.nyan.xyz/httpapi/sexphoto"},
		}
		writeConfig(config)
	} else {
		log.Fatalln("error:", err)
	}
	return config
}

func getExtension(contentType string) string {
	if contentType == "image/jpeg" {
		return "jpg"
	}
	if contentType == "image/png" {
		return "png"
	}
	return ""
}

func getImgURL(response string) string {
	// Use regex to extract image url from http response
	// Example: https://api.nyan.xyz/httpapi/sexphoto
	response = strings.Replace(response, `\/`, "/", -1)
	pattern := regexp.MustCompile(`https?\:\/\/.+\.(?i)(jpg|jpeg|png)`)
	return pattern.FindString(response)
}

func getImgExtension(filename string) string {
	// Get image extension from url
	pattern := regexp.MustCompile(`.+\.(?i)(jpg|jpeg|png)$`)
	match := pattern.FindStringSubmatch(filename)
	if len(match) >= 2 {
		return strings.ToLower(match[1])
	}
	return ""
}

func downloadFile(filepath string, url string) error {

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func compressImage(data []byte) ([]byte, error) {
	imgSrc, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, err
	}
	newImg := image.NewRGBA(imgSrc.Bounds())
	draw.Draw(newImg, newImg.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(newImg, newImg.Bounds(), imgSrc, imgSrc.Bounds().Min, draw.Over)
	buf := bytes.Buffer{}
	err = jpeg.Encode(&buf, newImg, &jpeg.Options{Quality: ImageCompressQuality})
	if err != nil {
		return data, err
	}
	if buf.Len() > len(data) {
		return data, nil
	}
	return buf.Bytes(), nil
}

func isImage(filename string) bool {
	return getImgExtension(filename) != ""
}

// Handle requests

var config Config
var timestamp int64

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// Make sure client is using GET request
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// If requesting favicon.ico, return 404
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}

	// If requesting image, return image
	if strings.HasPrefix(r.URL.Path, "/"+config.CacheFolder+"/") {
		// Make sure the requesting filename is of one of .jpg/.jpeg/.png
		extension := getImgExtension(r.URL.Path)
		if extension == "" {
			http.NotFound(w, r)
			return
		}

		// Get image from cache
		filepath := config.CacheFolder + "/" + r.URL.Path[len(config.CacheFolder)+1:]
		if _, err := os.Stat(filepath); err == nil {
			// Image exists, return it
			http.ServeFile(w, r, filepath)
			return
		} else {
			// Image doesn't exist, return 404
			http.NotFound(w, r)
			return
		}
	}

	// All other request paths except / are discarded
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get hostname in request
	hostname := r.Host
	// Determine to use local or remote mode
	if config.Mode == Local || time.Now().Unix()-timestamp < config.UpdateInterval {
		// Local mode
		// Get random image from local folder
		files, err := ioutil.ReadDir(config.CacheFolder)
		if err != nil {
			fmt.Println("error:", err)
		} else {
			if len(files) == 0 {
				fmt.Println("error:", "No images in cache folder")
			} else {
				rand.Seed(time.Now().Unix())
				file := files[rand.Intn(len(files))]
				// Make sure the file is an image
				for !isImage(file.Name()) {
					// Remove the non-image file
					_ = os.Remove(config.CacheFolder + string(os.PathSeparator) + file.Name())
					file = files[rand.Intn(len(files))]
				}
				if !isImage(file.Name()) {
					fmt.Println("error:", "No images in cache folder")
				} else {
					// Serve image
					fmt.Println("Using local image: ", file.Name())
					fmt.Fprintf(w, "http://%s/%s/%s", hostname, config.CacheFolder, file.Name())
					return
				}
			}
		}
	}

	// Get a random remote from config.Remotes
	remote := config.Remotes[rand.Intn(len(config.Remotes))]
	fmt.Println("Using remote: ", remote)

	// Send get request to remote
	response, err := http.Get(remote)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer response.Body.Close()

	// Validate response status code
	if response.StatusCode != 200 && response.StatusCode != 302 && response.StatusCode != 301 {
		fmt.Println("error:", response.StatusCode)
		return
	}

	// Get response content type and decide whether to extract image url from response body
	var imgURL string
	contentType := response.Header.Get("Content-Type")
	extension := getExtension(contentType)
	if extension != "" {
		// Directly download image from remote
		imgURL = remote
	} else {
		// Extract image url from response body
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		imgURL = getImgURL(string(body))
		extension = getImgExtension(imgURL)
		fmt.Println("Using image url: ", imgURL)
	}
	// Filename with no extension before compressing
	filenameNoExtension := string(config.CacheFolder + string(os.PathSeparator) + strconv.FormatInt(timestamp, 10))
	filenameUncompressed := filenameNoExtension + "." + extension
	fmt.Println("Downloading uncompressed image to: ", filenameUncompressed)
	// Check if cache folder exists
	if _, err := os.Stat(config.CacheFolder); os.IsNotExist(err) {
		// Create cache folder
		err = os.Mkdir(config.CacheFolder, 0755)
		if err != nil {
			log.Fatalln("error:", err)
			return
		}
	}
	err = downloadFile(filenameUncompressed, imgURL)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// Compress image
	data, err := ioutil.ReadFile(filenameUncompressed)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	data, err = compressImage(data)
	filenameCompressed := filenameNoExtension + "_compressed.jpg"
	err = ioutil.WriteFile(filenameCompressed, data, 0644)
	fmt.Println("Writing compressed image to: ", filenameCompressed)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// Remove temporary file
	err = os.Remove(filenameUncompressed)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// Update last update timestamp
	timestamp = time.Now().UnixNano()
	fmt.Println("Latest update timestamp: ", timestamp)
	fmt.Fprintf(w, "http://%s/%s", hostname, strings.Replace(filenameCompressed, "\\", "/", -1))
	return
}

func getConfigString(config Config) string {
	configString, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Sprintf("%+v\n", config)
	} else {
		return string(configString)
	}
}

func reloadConfig(w http.ResponseWriter, r *http.Request) {
	config = getConfig()
	fmt.Println("Reloaded config: ", getConfigString(config))
	fmt.Fprintf(w, "Config reloaded")
}

func main() {
	// Create/Read config file
	config = getConfig()
	fmt.Println("Config: ", getConfigString(config))
	// Record last update timestamp
	timestamp = time.Now().Unix()

	http.HandleFunc("/", handleRequest)
	http.HandleFunc("/reload", reloadConfig)
	fmt.Println("Listening on port: ", config.ListenPort)
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(config.ListenPort), nil))
}
