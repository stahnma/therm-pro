package chart

import (
	"bytes"
	"fmt"
	"time"

	chart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"

	"github.com/stahnma/therm-pro/internal/cook"
)

// Probe colors matching the web UI
var probeColors = [4]drawing.Color{
	{R: 255, G: 99, B: 71, A: 255},  // Probe 1 - tomato red
	{R: 30, G: 144, B: 255, A: 255}, // Probe 2 - dodger blue
	{R: 50, G: 205, B: 50, A: 255},  // Probe 3 - lime green
	{R: 255, G: 165, B: 0, A: 255},  // Probe 4 - orange
}

var darkBG = drawing.Color{R: 30, G: 30, B: 30, A: 255}
var lightText = drawing.Color{R: 200, G: 200, B: 200, A: 255}
var gridLine = drawing.Color{R: 60, G: 60, B: 60, A: 255}

// RenderSessionChart generates a PNG chart of session temperature history.
// Returns PNG bytes. Only connected probes with valid data are plotted.
func RenderSessionChart(history []cook.Reading, probes [4]cook.Probe) ([]byte, error) {
	var series []chart.Series

	for i := 0; i < cook.NumProbes; i++ {
		var xVals []time.Time
		var yVals []float64

		for _, r := range history {
			temp := r.Temps[i]
			if temp == cook.ProbeDisconnected || temp == cook.ProbeError || temp == cook.ProbeOverTemp {
				continue
			}
			xVals = append(xVals, r.Timestamp)
			yVals = append(yVals, temp)
		}

		if len(xVals) == 0 {
			continue
		}

		series = append(series, chart.TimeSeries{
			Name:    probes[i].Label,
			XValues: xVals,
			YValues: yVals,
			Style: chart.Style{
				StrokeColor: probeColors[i],
				StrokeWidth: 2,
			},
		})

		// Add target temp line if configured
		if probes[i].Alert.TargetTemp != nil {
			series = append(series, chart.AnnotationSeries{
				Annotations: []chart.Value2{
					{
						XValue: float64(xVals[len(xVals)-1].UnixNano()),
						YValue: *probes[i].Alert.TargetTemp,
						Label:  fmt.Sprintf("Target: %.0f°F", *probes[i].Alert.TargetTemp),
					},
				},
				Style: chart.Style{
					StrokeColor:     probeColors[i],
					StrokeDashArray: []float64{5, 3},
				},
			})
		}
	}

	// Determine session start time for elapsed-hour x-axis labels
	var sessionStart time.Time
	for _, s := range series {
		if ts, ok := s.(chart.TimeSeries); ok && len(ts.XValues) > 0 {
			if sessionStart.IsZero() || ts.XValues[0].Before(sessionStart) {
				sessionStart = ts.XValues[0]
			}
		}
	}

	// If no series have data, create a minimal empty chart.
	// go-chart requires at least 2 x-values to render.
	if len(series) == 0 {
		now := time.Now()
		series = append(series, chart.TimeSeries{
			XValues: []time.Time{now.Add(-time.Minute), now},
			YValues: []float64{0, 0},
			Style:   chart.Style{StrokeColor: darkBG}, // invisible
		})
	}

	graph := chart.Chart{
		Width:  800,
		Height: 400,
		Background: chart.Style{
			FillColor: darkBG,
		},
		Canvas: chart.Style{
			FillColor: darkBG,
		},
		XAxis: chart.XAxis{
			Style: chart.Style{
				FontColor: lightText,
			},
			Ticks:          generateHourTicks(sessionStart, history),
			GridMajorStyle: chart.Style{
				StrokeColor: gridLine,
				StrokeWidth: 1,
			},
		},
		YAxis: chart.YAxis{
			Name: "°F",
			NameStyle: chart.Style{
				FontColor: lightText,
			},
			Style: chart.Style{
				FontColor: lightText,
			},
			GridMajorStyle: chart.Style{
				StrokeColor: gridLine,
				StrokeWidth: 1,
			},
		},
		Series: series,
	}

	// Add legend if we have named series
	graph.Elements = []chart.Renderable{
		chart.LegendLeft(&graph, chart.Style{
			FillColor: darkBG,
			FontColor: lightText,
		}),
	}

	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}
	return buf.Bytes(), nil
}

// generateHourTicks creates tick marks at adaptive intervals from session start.
// Under 2h: 30min ticks. 2-8h: 1h ticks. Over 8h: 2h ticks.
func generateHourTicks(sessionStart time.Time, history []cook.Reading) []chart.Tick {
	if len(history) == 0 || sessionStart.IsZero() {
		return nil
	}

	var lastTime time.Time
	for _, r := range history {
		if r.Timestamp.After(lastTime) {
			lastTime = r.Timestamp
		}
	}

	duration := lastTime.Sub(sessionStart)
	var interval time.Duration
	switch {
	case duration < 2*time.Hour:
		interval = 30 * time.Minute
	case duration < 8*time.Hour:
		interval = time.Hour
	default:
		interval = 2 * time.Hour
	}

	var ticks []chart.Tick
	for offset := time.Duration(0); offset <= duration+interval; offset += interval {
		t := sessionStart.Add(offset)
		h := int(offset.Hours())
		m := int(offset.Minutes()) % 60
		var label string
		if m == 0 {
			label = fmt.Sprintf("%dh", h)
		} else {
			label = fmt.Sprintf("%dh%dm", h, m)
		}
		ticks = append(ticks, chart.Tick{
			Value: chart.TimeToFloat64(t),
			Label: label,
		})
	}
	return ticks
}
