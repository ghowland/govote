/* ------------------------------------------------------------------------------
*
*  # Fullcalendar basic options
*
*  Specific JS code additions for extra_fullcalendar_views.html and 
*  extra_fullcalendar_styling.html pages
*
*  Version: 1.0
*  Latest update: Aug 1, 2015
*
* ---------------------------------------------------------------------------- */

$(function() {


    // Add events
    // ------------------------------

    
    // Event background colors
    var eventBackgroundColors = [
        {
            id: 10,
            title: 'Rauf, Thomas',
            start: '2016-11-30',
            end: '2016-12-06'
        },
        {
            id: 10,
            title: 'Thomas, Bret',
            start: '2016-12-06',
            end: '2016-12-13'
        },
        {
            id: 10,
            title: 'Bret, Geoff',
            start: '2016-12-13',
            end: '2016-12-20'
        },
        {
            id: 10,
            title: 'Geoff, Weilu',
            start: '2016-12-20',
            end: '2016-12-27'
        },
        {
            id: 1001,
            title: 'Vacation',
            start: '2016-12-26',
            end: '2016-12-31',
            color: '#FFCCBC',
            rendering: 'background'
        },
        {
            id: 10,
            title: 'Weilu, Rauf',
            start: '2016-12-27',
            end: '2017-01-03'
        }
    ];



    // Initialization
    // ------------------------------



    // Event background colors
    $('.fullcalendar-background-colors').fullCalendar({
        header: {
            left: 'prev,next today',
            center: 'title',
            right: 'month,agendaWeek,agendaDay'
        },
        defaultDate: '2016-12-19',
        editable: true,
        events: eventBackgroundColors
    });

});
