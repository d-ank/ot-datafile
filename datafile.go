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
	close  chan struct{}
	mutex  sync.Mutex
	path   string
	time   time.Time
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
	for i = 0; i <= len(str)/250; i++ {
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

// encodesValue simply hex encodes as a well as stripping newlines
func encodeValue(str string) string {
	b := hex.EncodeToString([]byte(strings.Replace(str, "\n", "", -1)))

	return string(b)
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

// keys is the keys you want to append the key to
func setKey(keys []string, name, data string) []string {
	i, err := findKey(keys, name)
	if err != nil {
		keys = append(keys, fnvHash(name)+encodeValue(data))
	} else {
		keys[i] = fnvHash(name) + encodeValue(data)
	}
	return keys
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

	return loc + `\ot\scripts\`, nil
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
func (hook *Hook) Write(json json.RawMessage) error {
	defer func() {
		stat, _ := os.Stat(hook.path)
		hook.time = stat.ModTime()
	}()
	data := cordWood(base64.RawStdEncoding.EncodeToString(json), 250)
	file, err := os.OpenFile(hook.path, os.O_SYNC|os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	keys := make([]string, 0)
	for scanner.Scan() {
		keys = append(keys, scanner.Text())
	}
	var meta *gabs.Container
	metaData, err := getKeyValue(keys, "meta")
	if err == nil {
		meta, err = gabs.ParseJSON([]byte(metaData))
		if err != nil {
			return err
		}
	} else {
		meta = gabs.New()
	}
	_, err = meta.Set(len(data), "in-max")
	if err != nil {
		return err
	}
	for i, v := range data {
		keys = setKey(keys, "in-"+strconv.Itoa(i), v)
	}
	keys = setKey(keys, "meta", meta.String())
	datawriter := bufio.NewWriter(file)
	defer datawriter.Flush()
	for _, data := range keys {
		println(data)
		_, _ = datawriter.WriteString(data + "\n")
	}
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
	scriptLocation, err := getScriptLocation()
	if err != nil {
		return nil, err
	}
	hook := Hook{
		Name:   datafile,
		Reader: make(chan json.RawMessage),
		close:  make(chan struct{}),
		path:   scriptLocation + datafile,
	}
	// pass back to the reader channel and writes to the datafile
	go func(hook *Hook) {
		for {
			select {
			default:
				stat, err := os.Stat(hook.path)
				if err != nil {
					time.Sleep(time.Millisecond * 2000)
					continue
				}
				if !stat.ModTime().After(hook.time) {
					time.Sleep(time.Millisecond / 2)
					continue
				}
				hook.time = stat.ModTime()
				time.Sleep(time.Microsecond)
				// this is awful, find a way around this (if delay is allowed just parse the error)
			RETRY:
				data, err := ioutil.ReadFile(hook.path)
				if err != nil {
					// csgo still is writing, wait 5 microseconds (ghetto)
					time.Sleep(time.Microsecond * 4)
					goto RETRY
				}
				json, err := Parse(data)
				if err != nil {
					println("parse err: " + err.Error())
					continue
				}
				hook.Reader <- json
			case <-hook.close:
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
