#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <NimBLEDevice.h>
#include "config.h"
#include "thermopro.h"

ThermoPro tp;

void connectBLE() {
    digitalWrite(LED_PIN, LOW);
    while (!tp.scan()) {
        Serial.println("Retrying scan in 5s...");
        delay(5000);
    }
    while (!tp.connect()) {
        Serial.println("Retrying connect in 5s...");
        delay(5000);
    }
    tp.sendHandshake();
    Serial.println("ThermoPro connected and streaming");
    digitalWrite(LED_PIN, HIGH);
}

void setup() {
    Serial.begin(115200);
    pinMode(LED_PIN, OUTPUT);

    // Connect to WiFi
    WiFi.begin(WIFI_SSID, WIFI_PASS);
    Serial.print("Connecting to WiFi");
    while (WiFi.status() != WL_CONNECTED) {
        delay(500);
        Serial.print(".");
        digitalWrite(LED_PIN, !digitalRead(LED_PIN));
    }
    Serial.printf("\nConnected: %s\n", WiFi.localIP().toString().c_str());

    // Initialize BLE
    NimBLEDevice::init("");

    // Connect to ThermoPro
    connectBLE();
}

void loop() {
    // Reconnect if disconnected
    if (!tp.isConnected()) {
        Serial.println("Disconnected, reconnecting...");
        connectBLE();
    }

    ProbeData data = tp.getLatestData();
    if (data.valid) {
        // Build JSON payload
        String json = "{\"probes\":[";
        for (int i = 0; i < NUM_PROBES; i++) {
            if (i > 0) json += ",";
            json += "{\"id\":" + String(i + 1) + ",\"temp_f\":" + String(data.temps[i], 1) + "}";
        }
        json += "],\"battery\":" + String(data.battery) + "}";

        // POST to server
        HTTPClient http;
        String url = String(SERVER_URL) + "/api/data";
        http.begin(url);
        http.addHeader("Content-Type", "application/json");
        int code = http.POST(json);
        if (code != 200) {
            Serial.printf("POST failed: %d\n", code);
        }
        http.end();
    }

    delay(3000);
}
