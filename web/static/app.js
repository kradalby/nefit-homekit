// Thermostat UI Controller
// Handles real-time updates via Server-Sent Events and user interactions

(function () {
    'use strict';

    // DOM element references
    const elements = {
        currentTemp: document.getElementById('current-temp'),
        targetTemp: document.getElementById('target-temp'),
        tempSlider: document.getElementById('temp-slider'),
        heatingStatus: document.getElementById('heating-status'),
        heatModeButton: document.querySelector("button[name='mode'][value='heat']"),
        offModeButton: document.querySelector("button[name='mode'][value='off']")
    };

    // Validate required elements exist
    function validateElements() {
        const required = ['currentTemp', 'targetTemp', 'tempSlider', 'heatingStatus'];
        const missing = required.filter(key => !elements[key]);

        if (missing.length > 0) {
            console.error('Missing required DOM elements:', missing);
            return false;
        }
        return true;
    }

    // Update current temperature display
    function updateCurrentTemperature(temp) {
        if (typeof temp !== 'number' || isNaN(temp)) {
            console.warn('Invalid current temperature:', temp);
            return;
        }
        elements.currentTemp.textContent = temp.toFixed(1) + '°C';
    }

    // Update target temperature display and slider
    function updateTargetTemperature(temp) {
        if (typeof temp !== 'number' || isNaN(temp)) {
            console.warn('Invalid target temperature:', temp);
            return;
        }

        const formatted = temp.toFixed(1);
        elements.tempSlider.value = formatted;
        elements.targetTemp.textContent = formatted + '°C';
    }

    // Update heating status indicator
    function updateHeatingStatus(isHeating) {
        if (typeof isHeating !== 'boolean') {
            console.warn('Invalid heating status:', isHeating);
            return;
        }

        if (isHeating) {
            elements.heatingStatus.textContent = 'Heating';
            elements.heatingStatus.className = 'status-heating';
        } else {
            elements.heatingStatus.textContent = 'Off';
            elements.heatingStatus.className = 'status-off';
        }
    }

    // Update mode button states
    function updateModeButtons(mode) {
        if (!elements.heatModeButton || !elements.offModeButton) {
            return; // Mode buttons are optional
        }

        if (typeof mode !== 'string') {
            console.warn('Invalid mode:', mode);
            return;
        }

        // Remove active class from both buttons first
        elements.heatModeButton.classList.remove('active');
        elements.offModeButton.classList.remove('active');

        // Add active class to the appropriate button
        if (mode === 'heat') {
            elements.heatModeButton.classList.add('active');
        } else if (mode === 'off') {
            elements.offModeButton.classList.add('active');
        } else {
            console.warn('Unknown mode:', mode);
        }
    }

    // Handle SSE message
    function handleStateUpdate(data) {
        if (!data || typeof data !== 'object') {
            console.error('Invalid state update data:', data);
            return;
        }

        // Update each component if the data is present
        if ('current_temperature' in data) {
            updateCurrentTemperature(data.current_temperature);
        }

        if ('target_temperature' in data) {
            updateTargetTemperature(data.target_temperature);
        }

        if ('heating_active' in data) {
            updateHeatingStatus(data.heating_active);
        }

        if ('mode' in data) {
            updateModeButtons(data.mode);
        }
    }

    // Initialize SSE connection
    function initializeSSE() {
        const eventSource = new EventSource('/events');

        eventSource.onmessage = function (event) {
            try {
                const data = JSON.parse(event.data);
                handleStateUpdate(data);
            } catch (err) {
                console.error('Failed to parse SSE message:', err, event.data);
            }
        };

        eventSource.onerror = function (err) {
            console.error('SSE connection error:', err);
            // Browser will automatically reconnect
        };

        return eventSource;
    }

    // Initialize temperature slider interaction
    function initializeSlider() {
        elements.tempSlider.addEventListener('input', function (event) {
            const value = parseFloat(event.target.value);
            if (!isNaN(value)) {
                elements.targetTemp.textContent = value.toFixed(1) + '°C';
            }
        });
    }

    // Initialize the application
    function init() {
        if (!validateElements()) {
            console.error('Cannot initialize: missing required elements');
            return;
        }

        initializeSlider();
        initializeSSE();
    }

    // Start when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
