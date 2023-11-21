package main

// Export nvme smart-log metrics in prometheus format

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tidwall/gjson"
)

var labels = []string{"device", "model"}

type nvmeCollector struct {
	nvmeCriticalWarning                    *prometheus.Desc
	nvmeTemperature                        *prometheus.Desc
	nvmeAvailSpare                         *prometheus.Desc
	nvmeSpareThresh                        *prometheus.Desc
	nvmePercentUsed                        *prometheus.Desc
	nvmeEnduranceGrpCriticalWarningSummary *prometheus.Desc
	nvmeDataUnitsRead                      *prometheus.Desc
	nvmeDataUnitsWritten                   *prometheus.Desc
	nvmeHostReadCommands                   *prometheus.Desc
	nvmeHostWriteCommands                  *prometheus.Desc
	nvmeControllerBusyTime                 *prometheus.Desc
	nvmePowerCycles                        *prometheus.Desc
	nvmePowerOnHours                       *prometheus.Desc
	nvmeUnsafeShutdowns                    *prometheus.Desc
	nvmeMediaErrors                        *prometheus.Desc
	nvmeNumErrLogEntries                   *prometheus.Desc
	nvmeWarningTempTime                    *prometheus.Desc
	nvmeCriticalCompTime                   *prometheus.Desc
	nvmeThmTemp1TransCount                 *prometheus.Desc
	nvmeThmTemp2TransCount                 *prometheus.Desc
	nvmeThmTemp1TotalTime                  *prometheus.Desc
	nvmeThmTemp2TotalTime                  *prometheus.Desc
}

// nvme smart-log field descriptions can be found on page 181 of:
// Figure 207: SMART / Health Information Log Page
// https://nvmexpress.org/wp-content/uploads/NVM-Express-Base-Specification-2.0c-2022.10.04-Ratified.pdf

func newNvmeCollector() prometheus.Collector {
	return &nvmeCollector{
		nvmeCriticalWarning: prometheus.NewDesc(
			"nvme_critical_warning",
			"Critical Warning: This field indicates critical warnings for the state of the controller. Each bit\n"+
				"corresponds to a critical warning type; multiple bits may be set. If a bit is cleared to ‘0’, then\n"+
				"that critical warning does not apply. Critical warnings may result in an asynchronous event\n"+
				"notification to the host. Bits in this field represent the current associated state and are not\n"+
				"persistent. \n"+
				"Bit Definition\n"+
				"00 If set to ‘1’, then the available spare space has fallen below\nthe threshold.\n"+
				"01 If set to ‘1’, then a temperature is above an over\n"+
				"temperature threshold or below an under temperature\nthreshold (refer to section 5.14.1.4).\n"+
				"02 If set to ‘1’, then the NVM subsystem reliability has been\n"+
				"degraded due to significant media related errors or any\n"+
				"internal error that degrades NVM subsystem reliability.\n"+
				"03 If set to ‘1’, then the media has been placed in read only\nmode.\n"+
				"04 If set to ‘1’, then the volatile memory backup device has\n"+
				"failed. This field is only valid if the controller has a volatile\nmemory backup solution.\n"+
				"07:05 Reserved",
			labels,
			nil,
		),
		nvmeTemperature: prometheus.NewDesc(
			"nvme_temperature",
			"Composite Temperature: Contains a value corresponding to a temperature in Kelvins that\n"+
				"represents the current composite temperature of the controller and namespace(s) associated\n"+
				"with that controller. The manner in which this value is computed is implementation specific\n"+
				"and may not represent the actual temperature of any physical point in the NVM subsystem.\n"+
				"The value of this field may be used to trigger an asynchronous event (refer to section 5.27.1.3).\n"+
				"Warning and critical overheating composite temperature threshold values are reported by the\n"+
				"WCTEMP and CCTEMP fields in the Identify Controller data structure in Figure 275. ",
			labels,
			nil,
		),
		nvmeAvailSpare: prometheus.NewDesc(
			"nvme_avail_spare",
			"Available Spare Threshold: When the Available Spare falls below the threshold indicated in\n"+
				"this field, an asynchronous event completion may occur. The value is indicated as a\n"+
				"normalized percentage (0% to 100%). The values 101 to 255 are reserved.",
			labels,
			nil,
		),
		nvmeSpareThresh: prometheus.NewDesc(
			"nvme_spare_thresh",
			"Available Spare Threshold: When the Available Spare falls below the threshold indicated in\n"+
				"this field, an asynchronous event completion may occur. The value is indicated as a\n"+
				"normalized percentage (0 to 100%).",
			labels,
			nil,
		),
		nvmePercentUsed: prometheus.NewDesc(
			"nvme_percent_used",
			"Percentage Used: Contains a vendor specific estimate of the percentage of NVM subsystem\n"+
				"life used based on the actual usage and the manufacturer’s prediction of NVM life. A value of\n"+
				"100 indicates that the estimated endurance of the NVM in the NVM subsystem has been\n"+
				"consumed, but may not indicate an NVM subsystem failure. The value is allowed to exceed\n"+
				"100. Percentages greater than 254 shall be represented as 255. This value shall be updated\n"+
				"once per power-on hour (when the controller is not in a sleep state).\n"+
				"Refer to the JEDEC JESD218A standard for SSD device life and endurance measurement\n"+
				"techniques.",
			labels,
			nil,
		),
		nvmeEnduranceGrpCriticalWarningSummary: prometheus.NewDesc(
			"nvme_endurance_grp_critical_warning_summary",
			"Endurance Group Critical Warning Summary: This field indicates critical warnings for the\n"+
				"state of Endurance Groups. Each bit corresponds to a critical warning type, multiple bits may\n"+
				"be set to ‘1’. If a bit is cleared to ‘0’, then that critical warning does not apply to any Endurance\n"+
				"Group. Critical warnings may result in an asynchronous event notification to the host. Bits in\n"+
				"this field represent the current associated state and are not persistent.\n"+
				"If a bit is set to ‘1’ in one or more Endurance Groups, then the corresponding bit shall be set\n"+
				"to ‘1’ in this field.\n"+
				"Bits Definition\n"+
				"7:4 Reserved\n"+
				"3 If set to ‘1’, then the namespaces in one or more Endurance Groups have been\n"+
				"placed in read only mode not as a result of a change in the write protection state\n"+
				"of a namespace (refer to section 8.12.1).\n"+
				"2 If set to ‘1’, then the reliability of one or more Endurance Groups has been\n"+
				"degraded due to significant media related errors or any internal error that\n"+
				"degrades NVM subsystem reliability.\n"+
				"1 Reserved\n"+
				"0 If set to ‘1’, then the available spare capacity of one or more Endurance Groups\n"+
				"has fallen below the threshold.",
			labels,
			nil,
		),
		nvmeDataUnitsRead: prometheus.NewDesc(
			"nvme_data_units_read",
			"Data Units Read: Contains the number of 512 byte data units the host has read from the\n"+
				"controller; this value does not include metadata. This value is reported in thousands (i.e., a\n"+
				"value of 1 corresponds to 1000 units of 512 bytes read) and is rounded up. When the LBA\n"+
				"size is a value other than 512 bytes, the controller shall convert the amount of data read to\n"+
				"512 byte units.\n"+
				"For the NVM command set, logical blocks read as part of Compare and Read operations shall\n"+
				"be included in this value.",
			labels,
			nil,
		),
		nvmeDataUnitsWritten: prometheus.NewDesc(
			"nvme_data_units_written",
			"Data Units Written: Contains the number of 512 byte data units the host has written to the\n"+
				"controller; this value does not include metadata. This value is reported in thousands (i.e., a\n"+
				"value of 1 corresponds to 1000 units of 512 bytes written) and is rounded up. When the LBA\n"+
				"size is a value other than 512 bytes, the controller shall convert the amount of data written to\n"+
				"512 byte units.\n"+
				"For the NVM command set, logical blocks written as part of Write operations shall be included\n"+
				"in this value. Write Uncorrectable commands shall not impact this value.",
			labels,
			nil,
		),
		nvmeHostReadCommands: prometheus.NewDesc(
			"nvme_host_read_commands",
			"Host Read Commands: Contains the number of read commands completed by the controller.\n"+
				"For the NVM command set, this is the number of Compare and Read commands.",
			labels,
			nil,
		),
		nvmeHostWriteCommands: prometheus.NewDesc(
			"nvme_host_write_commands",
			"Host Write Commands: Contains the number of write commands completed by the\n"+
				"controller.\n"+
				"For the NVM command set, this is the number of Write commands.",
			labels,
			nil,
		),
		nvmeControllerBusyTime: prometheus.NewDesc(
			"nvme_controller_busy_time",
			"Controller Busy Time: Contains the amount of time the controller is busy with I/O commands.\n"+
				"The controller is busy when there is a command outstanding to an I/O Queue (specifically, a\n"+
				"command was issued via an I/O Submission Queue Tail doorbell write and the corresponding\n"+
				"completion queue entry has not been posted yet to the associated I/O Completion Queue).\n"+
				"This value is reported in minutes.",
			labels,
			nil,
		),
		nvmePowerCycles: prometheus.NewDesc(
			"nvme_power_cycles",
			"Power Cycles: Contains the number of power cycles.",
			labels,
			nil,
		),
		nvmePowerOnHours: prometheus.NewDesc(
			"nvme_power_on_hours",
			"Power On Hours: Contains the number of power-on hours. This may not include time that\n"+
				"the controller was powered and in a non-operational power state.",
			labels,
			nil,
		),
		nvmeUnsafeShutdowns: prometheus.NewDesc(
			"nvme_unsafe_shutdowns",
			"Unsafe Shutdowns: Contains the number of unsafe shutdowns. This count is incremented\n"+
				"when a shutdown notification (CC.SHN) is not received prior to loss of power.",
			labels,
			nil,
		),
		nvmeMediaErrors: prometheus.NewDesc(
			"nvme_media_errors",
			"Media and Data Integrity Errors: Contains the number of occurrences where the controller\n"+
				"detected an unrecovered data integrity error. Errors such as uncorrectable ECC, CRC\n"+
				"checksum failure, or LBA tag mismatch are included in this field.",
			labels,
			nil,
		),
		nvmeNumErrLogEntries: prometheus.NewDesc(
			"nvme_num_err_log_entries",
			"Number of Error Information Log Entries: Contains the number of Error Information log\n"+
				"entries over the life of the controller.",
			labels,
			nil,
		),
		nvmeWarningTempTime: prometheus.NewDesc(
			"nvme_warning_temp_time",
			"Warning Composite Temperature Time: Contains the amount of time in minutes that the\n"+
				"controller is operational and the Composite Temperature is greater than or equal to the\n"+
				"Warning Composite Temperature Threshold (WCTEMP) field and less than the Critical\n"+
				"Composite Temperature Threshold (CCTEMP) field in the Identify Controller data structure in\n"+
				"Figure 90.\n"+
				"If the value of the WCTEMP or CCTEMP field is 0h, then this field is always cleared to 0h\n"+
				"regardless of the Composite Temperature value",
			labels,
			nil,
		),
		nvmeCriticalCompTime: prometheus.NewDesc(
			"nvme_critical_comp_time",
			"Critical Composite Temperature Time: Contains the amount of time in minutes that the\n"+
				"controller is operational and the Composite Temperature is greater the Critical Composite\n"+
				"Temperature Threshold (CCTEMP) field in the Identify Controller data structure in Figure 90.\n"+
				"If the value of the CCTEMP field is 0h, then this field is always cleared to 0h regardless of the\n"+
				"Composite Temperature value.",
			labels,
			nil,
		),
		nvmeThmTemp1TransCount: prometheus.NewDesc(
			"nvme_thm_temp1_trans_count",
			"Thermal Management Temperature 1 Transition Count: Contains the number of times the\n"+
				"controller transitioned to lower power active power states or performed vendor specific thermal\n"+
				"management actions while minimizing the impact on performance in order to attempt to\n"+
				"reduce the Composite Temperature because of the host controlled thermal management\n"+
				"feature (refer to section 8.15.5) (i.e., the Composite Temperature rose above the Thermal\n"+
				"Management Temperature 1). This counter shall not wrap once the value FFFFFFFFh is\n"+
				"reached. A value of 0h, indicates that this transition has never occurred or this field is not\n"+
				"implemented. ",
			labels,
			nil,
		),
		nvmeThmTemp2TransCount: prometheus.NewDesc(
			"nvme_thm_temp2_trans_count",
			"Thermal Management Temperature 2 Transition Count: Contains the number of times the\n"+
				"controller transitioned to lower power active power states or performed vendor specific thermal\n"+
				"management actions regardless of the impact on performance (e.g., heavy throttling) in order\n"+
				"to attempt to reduce the Composite Temperature because of the host controlled thermal\n"+
				"management feature (refer to section 8.15.5) (i.e., the Composite Temperature rose above\n"+
				"the Thermal Management Temperature 2). This counter shall not wrap once the value\n"+
				"FFFFFFFFh is reached. A value of 0h, indicates that this transition has never occurred or this\n"+
				"field is not implemented.",
			labels,
			nil,
		),
		nvmeThmTemp1TotalTime: prometheus.NewDesc(
			"nvme_thm_temp1_trans_time",
			"Total Time For Thermal Management Temperature 1: Contains the number of seconds that\n"+
				"the controller had transitioned to lower power active power states or performed vendor specific\n"+
				"thermal management actions while minimizing the impact on performance in order to attempt\n"+
				"to reduce the Composite Temperature because of the host controlled thermal management\n"+
				"feature (refer to section 8.15.5). This counter shall not wrap once the value FFFFFFFFh is\n"+
				"reached. A value of 0h, indicates that this transition has never occurred or this field is not\n"+
				"implemented.",
			labels,
			nil,
		),
		nvmeThmTemp2TotalTime: prometheus.NewDesc(
			"nvme_thm_temp2_trans_time",
			"Total Time For Thermal Management Temperature 2: Contains the number of seconds that\n"+
				"the controller had transitioned to lower power active power states or performed vendor specific\n"+
				"thermal management actions regardless of the impact on performance (e.g., heavy throttling)\n"+
				"in order to attempt to reduce the Composite Temperature because of the host controlled\n"+
				"thermal management feature (refer to section 8.15.5). This counter shall not wrap once the\n"+
				"value FFFFFFFFh is reached. A value of 0h, indicates that this transition has never occurred\n"+
				"or this field is not implemented.",
			labels,
			nil,
		),
	}
}

func (c *nvmeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nvmeCriticalWarning
	ch <- c.nvmeTemperature
	ch <- c.nvmeAvailSpare
	ch <- c.nvmeSpareThresh
	ch <- c.nvmePercentUsed
	ch <- c.nvmeEnduranceGrpCriticalWarningSummary
	ch <- c.nvmeDataUnitsRead
	ch <- c.nvmeDataUnitsWritten
	ch <- c.nvmeHostReadCommands
	ch <- c.nvmeHostWriteCommands
	ch <- c.nvmeControllerBusyTime
	ch <- c.nvmePowerCycles
	ch <- c.nvmePowerOnHours
	ch <- c.nvmeUnsafeShutdowns
	ch <- c.nvmeMediaErrors
	ch <- c.nvmeNumErrLogEntries
	ch <- c.nvmeWarningTempTime
	ch <- c.nvmeCriticalCompTime
	ch <- c.nvmeThmTemp1TransCount
	ch <- c.nvmeThmTemp2TransCount
	ch <- c.nvmeThmTemp1TotalTime
	ch <- c.nvmeThmTemp2TotalTime
}

func ToFloat(value gjson.Result) float64 {
	if value.Type == gjson.String {
		noCommas := strings.Replace(value.String(), ",", "", -1)
		f, err := strconv.ParseFloat(noCommas, 64)
		if err != nil {
			return 0
		}
		return f
	}

	return value.Float()
}

func (c *nvmeCollector) Collect(ch chan<- prometheus.Metric) {
	nvmeDeviceCmd, err := exec.Command("nvme", "list", "-o", "json").Output()
	if err != nil {
		log.Fatalf("Error running nvme command: %s\n", err)
	}
	if !gjson.Valid(string(nvmeDeviceCmd)) {
		log.Fatal("nvmeDeviceCmd json is not valid")
	}
	nvmeDeviceList := gjson.Get(string(nvmeDeviceCmd), "Devices.#.DevicePath")
	nvmeModelList := gjson.Get(string(nvmeDeviceCmd), "Devices.#.ModelNumber").Array()
	for idx, nvmeDevice := range nvmeDeviceList.Array() {
		nvmeModel := nvmeModelList[idx]
		nvmeSmartLog, err := exec.Command("nvme", "smart-log", nvmeDevice.String(), "-o", "json").Output()
		if err != nil {
			log.Fatalf("Error running nvme smart-log command for device %s: %s\n", nvmeDevice.String(), err)
		}
		if !gjson.Valid(string(nvmeSmartLog)) {
			log.Fatalf("nvmeSmartLog json is not valid for device: %s: %s\n", nvmeDevice.String(), err)
		}

		nvmeSmartLogMetrics := gjson.GetMany(string(nvmeSmartLog),
			"critical_warning",
			"temperature",
			"avail_spare",
			"spare_thresh",
			"percent_used",
			"endurance_grp_critical_warning_summary",
			"data_units_read",
			"data_units_written",
			"host_read_commands",
			"host_write_commands",
			"controller_busy_time",
			"power_cycles",
			"power_on_hours",
			"unsafe_shutdowns",
			"media_errors",
			"num_err_log_entries",
			"warning_temp_time",
			"critical_comp_time",
			"thm_temp1_trans_count",
			"thm_temp2_trans_count",
			"thm_temp1_total_time",
			"thm_temp2_total_time")

		ch <- prometheus.MustNewConstMetric(c.nvmeCriticalWarning, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[0]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeTemperature, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[1]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeAvailSpare, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[2]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeSpareThresh, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[3]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmePercentUsed, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[4]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeEnduranceGrpCriticalWarningSummary, prometheus.GaugeValue, ToFloat(nvmeSmartLogMetrics[5]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeDataUnitsRead, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[6]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeDataUnitsWritten, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[7]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeHostReadCommands, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[8]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeHostWriteCommands, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[9]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeControllerBusyTime, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[10]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmePowerCycles, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[11]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmePowerOnHours, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[12]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeUnsafeShutdowns, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[13]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeMediaErrors, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[14]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeNumErrLogEntries, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[15]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeWarningTempTime, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[16]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeCriticalCompTime, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[17]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeThmTemp1TransCount, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[18]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeThmTemp2TransCount, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[19]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeThmTemp1TotalTime, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[20]), nvmeDevice.String(), nvmeModel.String())
		ch <- prometheus.MustNewConstMetric(c.nvmeThmTemp2TotalTime, prometheus.CounterValue, ToFloat(nvmeSmartLogMetrics[21]), nvmeDevice.String(), nvmeModel.String())
	}
}

func main() {
	port := flag.String("port", "9998", "port to listen on")
	flag.Parse()
	// check user
	currentUser, err := user.Current()
	if err != nil {
		log.Fatalf("Error getting current user  %s\n", err)
	}
	if currentUser.Username != "root" {
		log.Fatalln("Error: you must be root to use nvme-cli")
	}
	// check for nvme-cli executable
	_, err = exec.LookPath("nvme")
	if err != nil {
		log.Fatalf("Cannot find nvme command in path: %s\n", err)
	}
	prometheus.MustRegister(newNvmeCollector())
	http.Handle("/metrics", promhttp.Handler())

	fmt.Print("Starting server on port " + *port + "\n")

	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
