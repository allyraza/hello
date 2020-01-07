package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"
)

type weatherProvider interface {
	temperature(city string) (float64, error)
}

// OpenWeatherMap
type openWeatherMap struct {
	apiKey string
}

func (owm openWeatherMap) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.openweathermap.org/data/2.5/weather?APPID=" + owm.apiKey + "&q=" + city)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Name string `json:"name"`
		Main struct {
			Kelvin float64 `json:"temp"`
		} `json:"main"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	log.Printf("openWeatherMap: city=%s, temperature=%.2f\n", city, d.Main.Kelvin)

	return d.Main.Kelvin, nil
}

// WeatherStack
type weatherStack struct {
	apiKey string
}

func (ws weatherStack) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.weatherstack.com/current?access_key=" + ws.apiKey + "&query=" + city)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var d struct {
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
		Current struct {
			Temperature float64 `json:"temperature"`
		} `json:"current"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	log.Printf("weatherStack: city=%s, temperature=%.2f\n", city, d.Current.Temperature)

	return d.Current.Temperature, nil
}

type multiWeatherProvider []weatherProvider

func (w multiWeatherProvider) temperature(city string) (float64, error) {

	tempc := make(chan float64, len(w))
	errorc := make(chan error, len(w))

	for _, provider := range w {
		go func(p weatherProvider) {
			k, err := p.temperature(city)
			if err != nil {
				errorc <- err
				return
			}
			tempc <- k
		}(provider)
	}

	sum := 0.0

	for i := 0; i < len(w); i++ {
		select {
		case k := <-tempc:
			sum += k
		case <-time.After(300 * time.Millisecond):
			return 0, errors.New("api time out")
		case err := <-errorc:
			return 0, err
		}
	}

	return sum / float64(len(w)), nil
}

func main() {
	var (
		weatherStackKey   = flag.String("weatherstack-key", "", "Weather stack api key.")
		openWeatherMapKey = flag.String("openweathermap-key", "", "Open weather map api key.")
		ddosEnabled       = flag.Bool("ddos", false, "Enable DDOS Mode")
	)
	flag.Parse()

	if len(*weatherStackKey) < 1 && len(*openWeatherMapKey) < 1 {
		flag.Usage()
		return
	}

	var perProviderReqCount int

	if *ddosEnabled {
		perProviderReqCount = 100000
	} else {
		perProviderReqCount = 1
	}

	var mw multiWeatherProvider

	for i := 0; i < perProviderReqCount; i++ {
		mw = append(mw, weatherStack{*weatherStackKey})
		mw = append(mw, openWeatherMap{*openWeatherMapKey})
	}

	http.HandleFunc("/hello", hello)

	http.HandleFunc("/weather/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		city := strings.SplitN(r.URL.Path, "/", 3)[2]

		d, err := mw.temperature(city)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		weatherResponse := map[string]interface{}{
			"name":        city,
			"temperature": d,
			"took":        time.Since(start).String(),
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(weatherResponse); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.ListenAndServe(":8080", nil)
}

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("hello!"))
}
