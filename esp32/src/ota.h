// esp32/src/ota.h
#ifndef OTA_H
#define OTA_H

#include "config.h"

// Check server for newer firmware and apply if available.
// Returns true if an update was applied (device will restart).
bool checkAndApplyOTA();

#endif
