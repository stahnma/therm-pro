// esp32/src/ota.cpp
#include "ota.h"
#include <HTTPClient.h>
#include <ArduinoJson.h>
#include <Update.h>
#include <WiFi.h>

bool checkAndApplyOTA() {
    HTTPClient http;
    String url = String(SERVER_URL) + "/api/firmware/latest";
    http.begin(url);
    int code = http.GET();

    if (code != 200) {
        Serial.println("OTA: no firmware available");
        http.end();
        return false;
    }

    String body = http.getString();
    http.end();

    JsonDocument doc;
    deserializeJson(doc, body);
    int remoteVersion = doc["version"];

    if (remoteVersion <= FIRMWARE_VERSION) {
        Serial.printf("OTA: firmware up to date (v%d)\n", FIRMWARE_VERSION);
        return false;
    }

    Serial.printf("OTA: updating from v%d to v%d\n", FIRMWARE_VERSION, remoteVersion);

    // Download and flash
    HTTPClient dl;
    dl.begin(String(SERVER_URL) + "/api/firmware/download");
    int dlCode = dl.GET();
    if (dlCode != 200) {
        Serial.println("OTA: download failed");
        dl.end();
        return false;
    }

    int contentLength = dl.getSize();
    WiFiClient* stream = dl.getStreamPtr();

    if (!Update.begin(contentLength)) {
        Serial.println("OTA: not enough space");
        dl.end();
        return false;
    }

    Update.writeStream(*stream);
    if (Update.end()) {
        Serial.println("OTA: success, rebooting");
        dl.end();
        ESP.restart();
        return true;
    }

    Serial.printf("OTA: failed: %s\n", Update.errorString());
    dl.end();
    return false;
}
