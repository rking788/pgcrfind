package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "math"
    "net/http"
    "time"
    "os"
)

const (
    apiKey = "246eef35c19942f580851a08ab4248bc"
)

var (
    recordByID map[int64]*PGCR
)

type PGCR struct {
    Response struct {
        Period *PGCRTimestamp `json:"period"`
        ActivityDetails *ActivityDetails `json:"activityDetails"`
    } `json:"Response"`
}

type ActivityDetails struct {
    InstanceID string `json:"instanceId"`
}

type PGCRTimestamp time.Time
func (t PGCRTimestamp) Equals(other PGCRTimestamp) bool {
    return time.Time(t).Equal(time.Time(other))
}

func (t *PGCRTimestamp) UnmarshalJSON(data []byte) error {
    var innerTime time.Time
    if err := json.Unmarshal(data, &innerTime); err != nil {
        return err
    }

    *t = PGCRTimestamp(innerTime)
    return nil
}

func (t PGCRTimestamp) Before(other PGCRTimestamp) bool {
    return time.Time(t).Before(time.Time(other))
}

func main() {
    timeFlag := flag.String("start", "", "The datetime to attempt to find a PGCR record")
    isCliFlag := flag.Bool("is-cli", false, "Whether this command is being run from in interactive mode or not. Default=false")

    flag.Parse()

    recordByID = make(map[int64]*PGCR)
    if *isCliFlag == false {
        http.HandleFunc("/pgcrfind", findler)
        fmt.Printf("Listening on port=:9000...\n")
        err := http.ListenAndServe(":9000", nil)
        fmt.Println(err.Error())
        os.Exit(1)
    } else {
        if timeFlag == nil {
            fmt.Printf("Forgot to specify a start time")
            os.Exit(1)
        }

        needleTime, err := time.Parse(time.RFC3339, *timeFlag)
        if err != nil {
            fmt.Println("Invalid timestamp format provided for the start argument")
            os.Exit(1)
        }

        record, isExactMatch := findID(PGCRTimestamp(needleTime))
        recordJSON, _ := json.Marshal(record)
        fmt.Printf("Found record with ID=%s, isMatch=%v\n%+v", record.Response.ActivityDetails.InstanceID, isExactMatch, string(recordJSON))
    }
}

func findler(w http.ResponseWriter, r *http.Request) {
    startString := r.URL.Query().Get("start")
    endString := r.URL.Query().Get("end")

    var err error
    var startTime time.Time
    var endTime time.Time

    if startString == "now" {
        startTime = time.Now()
    } else if endString == "now" {
        endTime = time.Now()
    } else if startString != "" {
        startTime, err = time.Parse(time.RFC3339, startString)
        if err != nil {
            fmt.Printf("Error parsing startString. [err:%s]", err.Error())
            w.WriteHeader(http.StatusBadRequest)
            return
        }
    } else if endString != "" {
        endTime, err = time.Parse(time.RFC3339, endString)
        if err != nil {
            fmt.Printf("Error parsing endString. [err:%s]", err.Error())
            w.WriteHeader(http.StatusBadRequest)
            return
        }
    } else {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    // TODO: Validate that these times aren't in the future

    fmt.Printf("User specified parameters. [startTime:%+v, endTime:%+v]", startTime, endTime)
    // TODO: Support searching by the end time instead of start
    record, isExactMatch := findID(PGCRTimestamp(startTime))

    if isExactMatch {
        w.Write([]byte(fmt.Sprintf("Found exact match ID=%s", record.Response.ActivityDetails.InstanceID)))
    } else {
        w.Write([]byte(fmt.Sprintf("Closest match ID=%s", record.Response.ActivityDetails.InstanceID)))
    }
}

func findID(needle PGCRTimestamp) (*PGCR, bool) {
    bottom := int64(1)
    top := int64(math.MaxInt64)
    currentID := int64(0)
    var current *PGCR
    var match *PGCR
    var closest *PGCR
    var err error

    for {

        difference := top - bottom
        increment := difference / 2
        if increment == 0 {
            increment = 1
        }

        currentID = bottom + increment
        fmt.Printf("Loading id=%d\n", currentID)
        if fromCache, ok := recordByID[currentID]; ok {
            current = fromCache
            fmt.Printf("Got %d from cache\n", currentID)
        } else {
            current, err = loadPGCR(currentID)
            if err != nil {
                fmt.Printf("Error loading PGCR(%d) -- %s\n", currentID, err.Error())
                continue
            }
        }

        if current == nil {
            // If not found, then we need to go lower since we are beyond the endpoint of the activities
            top = currentID
            continue
        }
        recordByID[currentID] = current

        if current.Response.Period.Equals(needle) {
            match = current
            break
        } else if current.Response.Period.Before(needle) {
            if bottom == currentID || difference == 1 {
                closest = current
                break
            }
            bottom = currentID
        } else {
            if top == currentID || difference == 1 {
                closest = current
                break
            }
            top = currentID
        }
    }

    if match != nil {
        return match, true
    } else {
        return closest, false
    }
}

func loadPGCR(id int64) (*PGCR, error) {

    client := http.DefaultClient
    req, _ := http.NewRequest("GET", fmt.Sprintf("https://stats.bungie.net/Platform/Destiny2/Stats/PostGameCarnageReport/%d/", id), nil)
    req.Header.Add("X-API-Key", apiKey)
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    } else if resp.StatusCode == http.StatusNotFound {
        return nil, nil
    } else if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("incorrect status code received from the api. [statusCode:%d]", resp.StatusCode)
    }

    var p PGCR
    bodyBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    err = json.Unmarshal(bodyBytes, &p)
    if err != nil {
        return nil, err
    }

    return &p, nil
}
