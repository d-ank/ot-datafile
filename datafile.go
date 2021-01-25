package otdata

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeffail/gabs"
	"golang.org/x/sys/windows/registry"
)

// Hook is a hooked onetap datafile
type Hook struct {
	Name   string
	Reader chan json.RawMessage
	Writer chan json.RawMessage
	close  chan struct{}
	mutex  sync.Mutex
}

func fnvHash(val string) string {
	hasher := fnv.New32a()
	hasher.Write([]byte(val))
	// convert the fnv hash of val to a string and return
	return strconv.FormatUint(uint64(hasher.Sum32()), 16)
}

func cordWood(str string, cordLen int) []string {
	if cordLen > len(str) {
		cordLen = len(str)
	}
	var pieces []string
	var i int
	for i = 0; i < len(str)/250; i++ {
		pieces = append(pieces, str[i*cordLen:cordLen])
	}
	return pieces
}

// decodeValue simply hex decodes as a well as stripping newlines
func decodeValue(str string) (string, error) {
	b, err := hex.DecodeString(strings.Replace(str, "\n", "", -1))
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// findKey takes in a string slice and key name, returns the keys index if found
func findKey(keys []string, name string) (int, error) {
	for i, v := range keys {
		hash := fnvHash(name)
		if v[0:8] == strings.ToUpper(hash) {
			return i, nil
		}
	}
	return 0, errors.New("Unable to find index")
}

func getScriptLocation() (string, error) {
	query, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Steam App 730`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer query.Close()
	loc, _, err := query.GetStringValue("InstallLocation")
	if err != nil {
		return "", err
	}
	return loc, nil
}

func getKeyValue(keys []string, name string) (string, error) {
	index, err := findKey(keys, name)
	if err != nil {
		return "", err
	}
	dec, err := decodeValue(keys[index][8:])
	if err != nil {
		return "", err
	}
	return dec, nil
}

// Write handles writing to a datafile, it is structurally the same as with the javascript library however it cant write to the out segment, instead it writes to the in segment
func (hook *Hook) Write(file string, json json.RawMessage) error {
	/*
		data, err := ioutil.ReadFile(file)
		if err != nil {
			time.Sleep(time.Microsecond * 4)
			data, _ = ioutil.ReadFile(file)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		var keys []string
		for scanner.Scan() {
			keys = append(keys, scanner.Text())
		}
		metaData, err := getKeyValue(keys, "meta")
		if err != nil {
			println(err.Error())
			return err
		}
		meta, err := gabs.ParseJSON([]byte(metaData))
		if err != nil {
			println(err.Error())
			return err
		}
		enc := base64.RawStdEncoding.EncodeToString(json)
		dataList := cordWood(enc, 250)
		_, err = meta.Set(len(dataList), "in-max")
		if err != nil {
			println(err.Error())
			return err
		}
		for i, v := range dataList {
			//index, err := findKey(dataList, "in-"+strconv.Itoa(i))
			if err != nil {
				hash := fnvHash("in-" + strconv.Itoa(i))
				dataList[i] = hash + enc
			}

		}
	*/
	return nil
}

// Parse outputs a json string from the onetap datafile format, so long as the data values are also a json
func Parse(str []byte) (json.RawMessage, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(str)))
	var max int
	var keys []string
	for scanner.Scan() {
		keys = append(keys, scanner.Text())
	}
	index, err := findKey(keys, "meta")
	if err != nil {
		return nil, err
	}
	dec, err := decodeValue(keys[index][8:])
	if err != nil {
		return nil, err
	}
	c, err := gabs.ParseJSON([]byte(dec))
	if err != nil {
		return nil, err
	}
	max = int(c.Search("out-max").Data().(float64))
	var data string
	for i := 0; i < max; i++ {
		index, err := findKey(keys, "out-"+strconv.Itoa(i))
		if err != nil {
			return nil, err
		}
		val, err := decodeValue(keys[index][8:])
		if err != nil {
			return nil, err
		}
		data += val
	}
	ret, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// Add takes in a datafile in the ot/scripts subfolder and returns a Hook
func Add(datafile string) (*Hook, error) {
	hook := Hook{
		Name:   datafile,
		Reader: make(chan json.RawMessage),
		close:  make(chan struct{}),
	}
	// pass back to the reader channel and writes to the datafile
	go func(hook *Hook) {
		var lastTime time.Time
		scriptLocation, err := getScriptLocation()
		if err != nil {
			hook.Close()
		}
		for {
			select {
			default:
				hook.mutex.Lock()
				defer hook.mutex.Unlock()
				stat, err := os.Stat(scriptLocation + `\ot\scripts\` + datafile)
				if err != nil {
					time.Sleep(time.Millisecond * 2000)
					continue
				}
				if !stat.ModTime().After(lastTime) {
					time.Sleep(time.Millisecond / 2)
					continue
				}
				lastTime = stat.ModTime()
				time.Sleep(time.Microsecond)
				// this is awful, find a way around this (if delay is allowed just parse the error)
			RETRY:
				data, err := ioutil.ReadFile(scriptLocation + `\ot\scripts\` + datafile)
				if err != nil {
					// csgo still is writing, wait 5 microseconds (ghetto)
					time.Sleep(time.Microsecond * 4)
					goto RETRY
				}
				json, err := Parse(data)
				if err != nil {
					println(err.Error())
					continue
				}
				hook.Reader <- json
			case data := <-hook.Writer:
				println(data)
				//Write(scriptLocation+`\ot\scripts\`+datafile, data)
			case <-hook.close:
				close(hook.Writer)
				close(hook.Reader)
				close(hook.close)
				return
			}
		}
	}(&hook)
	return &hook, nil
}

// Close the hook on the datafile
func (hook *Hook) Close() {
	<-hook.close
}
