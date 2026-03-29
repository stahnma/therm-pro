// esp32/src/main.cpp
#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include "config.h"

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
    digitalWrite(LED_PIN, HIGH);
}

void loop() {
    // Placeholder - will be filled in by Task 12
    delay(1000);
}
