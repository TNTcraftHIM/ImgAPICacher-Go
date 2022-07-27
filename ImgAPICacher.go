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
	Remote                      Mode   = "remote"
	ConfigDefaultName           string = "config.json"
	ConfigDefaultListenPort     int    = 8080
	ConfigDefaultCacheFolder    string = "cache"
	ConfigDefaultCacheTmpFolder string = "tmp"
	ConfigDefaultUpdateInterval int64  = 10
	ConfigDefaultMaxCacheSize   int    = 100
	ConfigDefaultImageQuality   int    = 60
	ConfigDefaultRemote1        string = "https://api.nyan.xyz/httpapi/sexphoto"
	ConfigDefaultRemote2        string = "https://loliapi.com/acg"
)

type Mode string

type Config struct {
	ListenPort     int
	Mode           Mode
	CacheFolder    string
	CacheTmpFolder string
	UpdateInterval int64
	MaxCacheSize   int
	ImageQuality   int
	Remotes        []string
}

func readConfig() Config {
	// Read config file
	file, err := ioutil.ReadFile(ConfigDefaultName)
	if err != nil {
		log.Fatalln("Error:", err)
	}
	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		log.Fatalln("Error:", err)
	}
	return config
}

func writeConfig(config Config) {
	// Write config struct to json file
	file, _ := json.MarshalIndent(config, "", "\t")
	err := ioutil.WriteFile(ConfigDefaultName, file, 0644)
	if err != nil {
		fmt.Println("Error:", err)
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
			CacheFolder:    ConfigDefaultCacheFolder,
			CacheTmpFolder: ConfigDefaultCacheTmpFolder,
			UpdateInterval: ConfigDefaultUpdateInterval,
			MaxCacheSize:   ConfigDefaultMaxCacheSize,
			ImageQuality:   ConfigDefaultImageQuality,
			Remotes:        []string{ConfigDefaultRemote1, ConfigDefaultRemote2},
		}
		writeConfig(config)
	} else {
		log.Fatalln("Error:", err)
	}
	return config
}

func reloadConfig(w http.ResponseWriter, r *http.Request) {
	config = getConfig()
	fmt.Println("Reloaded config: ", getConfigString(config))
	fmt.Fprintf(w, "Config reloaded")
}

func getConfigString(config Config) string {
	configString, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Sprintf("%+v\n", config)
	} else {
		return string(configString)
	}
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
	err = jpeg.Encode(&buf, newImg, &jpeg.Options{Quality: config.ImageQuality})
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

func retrieveRemote(hostname string, w http.ResponseWriter, r *http.Request) {
	// Get a random remote from config.Remotes
	remote := config.Remotes[rand.Intn(len(config.Remotes))]
	fmt.Println("Using remote: ", remote)

	// Send get request to remote
	response, err := http.Get(remote)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer response.Body.Close()

	// Validate response status code
	if response.StatusCode != 200 && response.StatusCode != 302 && response.StatusCode != 301 {
		fmt.Println("Error:", errors.New("Invalid response status code "+strconv.Itoa(response.StatusCode)))
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
			fmt.Println("Error:", err)
			return
		}
		imgURL = getImgURL(string(body))
		extension = getImgExtension(imgURL)
		fmt.Println("Using image url: ", imgURL)
	}
	// Filename with no extension before compressing
	filenameUncompressed := string(config.CacheFolder+string(os.PathSeparator)+config.CacheTmpFolder+string(os.PathSeparator)+strconv.FormatInt(time.Now().UnixMilli(), 10)) + "." + extension
	fmt.Println("Downloading uncompressed image to: ", filenameUncompressed)
	// Check if cache folder and its tmp folder exists
	if _, err := os.Stat(config.CacheFolder); os.IsNotExist(err) {
		// Create cache folder
		err = os.Mkdir(config.CacheFolder, 0755)
		if err != nil {
			log.Fatalln("Error:", err)
			return
		}
	}
	if _, err := os.Stat(config.CacheFolder + string(os.PathSeparator) + config.CacheTmpFolder); os.IsNotExist(err) {
		// Create tmp folder
		err = os.Mkdir(config.CacheFolder+string(os.PathSeparator)+config.CacheTmpFolder, 0755)
		if err != nil {
			log.Fatalln("Error:", err)
			return
		}
	}

	// Download image to tmp folder
	err = downloadFile(filenameUncompressed, imgURL)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Compress image
	data, err := ioutil.ReadFile(filenameUncompressed)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	// Save compressed image to cache folder
	data, err = compressImage(data)
	filenameCompressed := string(config.CacheFolder+string(os.PathSeparator)+strconv.FormatInt(time.Now().UnixMilli(), 10)) + ".jpg"
	err = ioutil.WriteFile(filenameCompressed, data, 0644)
	fmt.Println("Writing compressed image to: ", filenameCompressed)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	// Remove temporary file
	err = os.Remove(filenameUncompressed)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	// Update last update timestamp
	timestamp = time.Now().Unix()
	fmt.Println("Latest update timestamp: ", timestamp)

	// Check if current amount of image have reached the limit
	if config.MaxCacheSize != 0 {
		files, err := ioutil.ReadDir(config.CacheFolder)
		if err != nil {
			fmt.Println("Error:", err)
			return
		} else {
			if len(files) >= config.MaxCacheSize {
				// Change mode to local
				config.Mode = Local
				writeConfig(config)
				fmt.Println("Mode changed to local")
			}
		}
	}

	fmt.Fprintf(w, "http://%s/%s", hostname, strings.Replace(filenameCompressed, "\\", "/", -1))
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

	// Set cors headers
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// If requesting favicon.ico, return 404
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}

	// If requesting image, return image
	if strings.HasPrefix(r.URL.Path, "/"+config.CacheFolder+"/") {
		// Make sure the requesting filename is of one of supported extensions
		if getImgExtension(r.URL.Path) == "" {
			http.NotFound(w, r)
			return
		}

		// Get image from cache
		filepath := config.CacheFolder + string(os.PathSeparator) + r.URL.Path[len(config.CacheFolder)+1:]
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

	// Try to serve image from cache first
	served := false
	// Get random image from local folder
	files, err := ioutil.ReadDir(config.CacheFolder)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		if len(files) == 0 {
			fmt.Println("Error:", "No images in cache folder")
		} else {
			rand.Seed(time.Now().UnixMilli())
			file := files[rand.Intn(len(files))]
			// Make sure the file is an image
			for !isImage(file.Name()) {
				// Remove the non-image file
				err = os.Remove(config.CacheFolder + string(os.PathSeparator) + file.Name())
				if err != nil {
					fmt.Println("Error:", err)
				}
				file = files[rand.Intn(len(files))]
			}
			if !isImage(file.Name()) {
				fmt.Println("Error:", "No images in cache folder")
			} else {
				// Serve image
				fmt.Println("Using local image: ", file.Name())
				fmt.Fprintf(w, "http://%s/%s/%s", hostname, config.CacheFolder, file.Name())
				served = true
			}
		}
	}

	// Determine whether to access remote to retrieve image
	if served && (config.Mode == Local || time.Now().Unix()-timestamp < config.UpdateInterval) {
		return
	} else {
		if served {
			go func() {
				retrieveRemote(hostname, w, r)
			}()
		} else {
			retrieveRemote(hostname, w, r)
		}
	}
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
