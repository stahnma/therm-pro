#ifndef THERMOPRO_H
#define THERMOPRO_H

#include <NimBLEDevice.h>

// BLE UUIDs for ThermoPro TP25
#define TP25_SERVICE_UUID      "1086fff0-3343-4817-8bb2-b32206336ce8"
#define TP25_WRITE_CHAR_UUID   "1086fff1-3343-4817-8bb2-b32206336ce8"
#define TP25_NOTIFY_CHAR_UUID  "1086fff2-3343-4817-8bb2-b32206336ce8"

// Sentinel values
#define PROBE_DISCONNECTED -999.0f
#define PROBE_ERROR        -100.0f
#define PROBE_OVERTEMP      666.0f
#define NUM_PROBES          4

struct ProbeData {
    float temps[NUM_PROBES];
    int battery;
    bool valid;
};

class ThermoPro {
public:
    bool scan();
    bool connect();
    bool sendHandshake();
    ProbeData getLatestData();
    bool isConnected();

private:
    NimBLEAdvertisedDevice* device = nullptr;
    NimBLEClient* client = nullptr;
    NimBLERemoteCharacteristic* writeChar = nullptr;
    NimBLERemoteCharacteristic* notifyChar = nullptr;

    static ProbeData latestData;
    static bool dataReady;
    static void notifyCallback(NimBLERemoteCharacteristic* pChar,
                               uint8_t* pData, size_t length, bool isNotify);
    static float decodeBCD(uint8_t byte1, uint8_t byte2);
};

#endif
