/* ------------------------------------------------------------------------------
 *
 *  # Echarts - lines and areas
 *
 *  Lines and areas chart configurations
 *
 *  Version: 1.0
 *  Latest update: August 1, 2015
 *
 * ---------------------------------------------------------------------------- */

$(function() {


    // Set paths
    // ------------------------------

    require.config({
        paths: {
            echarts: 'assets/js/plugins/visualization/echarts'
        }
    });


    // Configuration
    // ------------------------------

    require(
        [
            'echarts',
            'echarts/theme/limitless',
            'echarts/chart/bar',
            'echarts/chart/line'
        ],


        // Charts setup
        function (ec, limitless) {


            // Initialize charts
            // ------------------------------

            var stacked_area = ec.init(document.getElementById('stacked_area'), limitless);
            var stacked_area2 = ec.init(document.getElementById('stacked_area2'), limitless);



            // Charts setup
            // ------------------------------


            //
            // Stacked area options
            //

            stacked_area_options = {

                // Setup grid
                grid: {
                    x: 40,
                    x2: 20,
                    y: 35,
                    y2: 25
                },

                // Add tooltip
                tooltip: {
                    trigger: 'axis'
                },

                // Add legend
                legend: {
                    data: ['DNS', 'Connecting', 'First Byte', 'Complete']
                },

                // Enable drag recalculate
                calculable: true,

                // Add horizontal axis 
                xAxis: [{
                    type: 'category',
                    boundaryGap: false,
                    data: ['00:00', '01:01', '02:02', '03:03', '04:04', '05:05', '06:06', '07:07', '08:08', '09:09', '10:10', '11:11', '00:12', '01:13', '02:14', '03:15', '04:16', '05:17', '06:18', '07:19', '08:20', '09:21', '10:22', '11:23', '00:24', '01:25', '02:26', '03:27', '04:28', '05:29', '06:30', '07:31', '08:32', '09:33', '10:34', '11:35', '00:36', '01:37', '02:38', '03:39', '04:40', '05:41', '06:42', '07:43', '08:44', '09:45', '10:46', '11:47', '00:48', '01:49']
                }],

                // Add vertical axis
                yAxis: [{
                    type: 'value'
                }],

                // Add series
                series: [
                    {
                        name: 'DNS',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [265, 273, 210, 267, 289, 102, 357, 115, 241, 237, 202, 372, 137, 277, 252, 204, 201, 342, 236, 356, 105, 339, 327, 394, 172, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
                    },
                    {
                        name: 'Connecting',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [265, 273, 210, 267, 289, 102, 357, 115, 241, 237, 202, 372, 137, 277, 252, 204, 201, 342, 236, 356, 105, 339, 327, 394, 172, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
                    },
                    {
                        name: 'First Byte',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [265, 273, 210, 267, 289, 102, 357, 115, 241, 237, 202, 372, 137, 277, 252, 204, 201, 342, 236, 356, 105, 339, 327, 394, 172, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
                    },
                    {
                        name: 'Complete',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [265, 273, 210, 267, 289, 102, 357, 115, 241, 237, 202, 372, 137, 277, 252, 204, 201, 342, 236, 356, 105, 339, 327, 394, 172, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
                    }
                ]
            };


            stacked_area_options2 = {

                // Setup grid
                grid: {
                    x: 40,
                    x2: 20,
                    y: 35,
                    y2: 25
                },

                // Add tooltip
                tooltip: {
                    trigger: 'axis'
                },

                // Add legend
                legend: {
                    data: ['DNS', 'Connecting', 'First Byte', 'Complete']
                },

                // Enable drag recalculate
                calculable: true,

                // Add horizontal axis 
                xAxis: [{
                    type: 'category',
                    boundaryGap: false,
                    data: ['00:00', '01:01', '02:02', '03:03', '04:04', '05:05', '06:06', '07:07', '08:08', '09:09', '10:10', '11:11', '00:12', '01:13', '02:14', '03:15', '04:16', '05:17', '06:18', '07:19', '08:20', '09:21', '10:22', '11:23', '00:24', '01:25', '02:26', '03:27', '04:28', '05:29', '06:30', '07:31', '08:32', '09:33', '10:34', '11:35', '00:36', '01:37', '02:38', '03:39', '04:40', '05:41', '06:42', '07:43', '08:44', '09:45', '10:46', '11:47', '00:48', '01:49']
                }],

                // Add vertical axis
                yAxis: [{
                    type: 'value'
                }],

                // Add series
                series: [
                    {
                        name: 'DNS',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 304, 236, 330, 393, 367, 183, 178, 262, 385, 272, 289, 136, 128, 366, 228, 128, 336, 160, 233, 395, 331, 209, 276, 318, 107]
                    },
                    {
                        name: 'Connecting',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 304, 236, 330, 393, 367, 183, 178, 262, 385, 272, 289, 136, 128, 366, 228, 128, 336, 160, 233, 395, 331, 209, 276, 318, 107]
                    },
                    {
                        name: 'First Byte',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 304, 236, 330, 393, 367, 183, 178, 262, 385, 272, 289, 136, 128, 366, 228, 128, 336, 160, 233, 395, 331, 209, 276, 318, 107]
                    },
                    {
                        name: 'Complete',
                        type: 'line',
                        stack: 'Total',
                        itemStyle: {normal: {areaStyle: {type: 'default'}}},
                        data: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 304, 236, 330, 393, 367, 183, 178, 262, 385, 272, 289, 136, 128, 366, 228, 128, 336, 160, 233, 395, 331, 209, 276, 318, 107]
                    }
                ]
            };


            // Apply options
            // ------------------------------

            stacked_area.setOption(stacked_area_options);
            stacked_area2.setOption(stacked_area_options2);



            // Resize charts
            // ------------------------------

            window.onresize = function () {
                setTimeout(function () {
                    stacked_area.resize();
                    stacked_area2.resize();
                }, 200);
            }
        }
    );
});
