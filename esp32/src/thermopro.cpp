#include "thermopro.h"

ProbeData ThermoPro::latestData = {};
bool ThermoPro::dataReady = false;

float ThermoPro::decodeBCD(uint8_t byte1, uint8_t byte2) {
    // Sentinel values
    if (byte1 == 0xFF && byte2 == 0xFF) return PROBE_DISCONNECTED;
    if (byte1 == 0xDD && byte2 == 0xDD) return PROBE_ERROR;
    if (byte1 == 0xEE && byte2 == 0xEE) return PROBE_OVERTEMP;

    bool negative = (byte1 & 0x80) != 0;
    float hundreds = ((byte1 >> 4) & 0x07) * 100.0f;
    float tens = (byte1 & 0x0F) * 10.0f;
    float ones = ((byte2 >> 4) & 0x0F);
    float tenths = (byte2 & 0x0F) * 0.1f;
    float temp = hundreds + tens + ones + tenths;
    return negative ? -temp : temp;
}

void ThermoPro::notifyCallback(NimBLERemoteCharacteristic* pChar,
                                uint8_t* pData, size_t length, bool isNotify) {
    if (length < 13 || pData[0] != 0x30) return;

    latestData.battery = pData[2];
    latestData.temps[0] = decodeBCD(pData[5], pData[6]);
    latestData.temps[1] = decodeBCD(pData[7], pData[8]);
    latestData.temps[2] = decodeBCD(pData[9], pData[10]);
    latestData.temps[3] = decodeBCD(pData[11], pData[12]);
    latestData.valid = true;
    dataReady = true;
}

bool ThermoPro::scan() {
    NimBLEScan* scan = NimBLEDevice::getScan();
    scan->setActiveScan(true);
    NimBLEScanResults results = scan->start(10);

    for (int i = 0; i < results.getCount(); i++) {
        NimBLEAdvertisedDevice adv = results.getDevice(i);
        if (adv.getName() == "Thermopro") {
            device = new NimBLEAdvertisedDevice(adv);
            Serial.println("Found ThermoPro TP25");
            return true;
        }
    }
    Serial.println("ThermoPro not found");
    return false;
}

bool ThermoPro::connect() {
    if (!device) return false;

    client = NimBLEDevice::createClient();
    if (!client->connect(device)) {
        Serial.println("BLE connect failed");
        return false;
    }

    NimBLERemoteService* svc = client->getService(TP25_SERVICE_UUID);
    if (!svc) {
        Serial.println("Service not found");
        client->disconnect();
        return false;
    }

    writeChar = svc->getCharacteristic(TP25_WRITE_CHAR_UUID);
    notifyChar = svc->getCharacteristic(TP25_NOTIFY_CHAR_UUID);

    if (!writeChar || !notifyChar) {
        Serial.println("Characteristics not found");
        client->disconnect();
        return false;
    }

    notifyChar->subscribe(true, notifyCallback);
    return true;
}

bool ThermoPro::sendHandshake() {
    if (!writeChar) return false;
    uint8_t handshake[] = {0x01, 0x09, 0x70, 0x32, 0xe2, 0xc1, 0x79, 0x9d, 0xb4, 0xd1, 0xc7, 0xb1};
    return writeChar->writeValue(handshake, sizeof(handshake), false);
}

ProbeData ThermoPro::getLatestData() {
    ProbeData data = latestData;
    dataReady = false;
    return data;
}

bool ThermoPro::isConnected() {
    return client && client->isConnected();
}
