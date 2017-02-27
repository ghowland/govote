/* ------------------------------------------------------------------------------
*
*  # Steps wizard
*
*  Specific JS code additions for wizard_steps.html page
*
*  Version: 1.1
*  Latest update: Dec 25, 2015
*
* ---------------------------------------------------------------------------- */

$(function() {


    // Wizard examples
    // ------------------------------



    // Specify custom starting step
    $(".steps-starting-step").steps({
        headerTag: "h6",
        bodyTag: "fieldset",
        startIndex: 0,
        titleTemplate: '<span class="number">#index#</span> #title#',
        autoFocus: true,
        onFinished: function (event, currentIndex) {
            alert("Form submitted.");
        }
    });

    // Initialize plugins
    // ------------------------------

    // Select2 selects
    $('.select').select2();


    // Simple select without search
    $('.select-simple').select2({
        minimumResultsForSearch: Infinity
    });


    // Styled checkboxes and radios
    $('.styled').uniform({
        radioClass: 'choice'
    });


    // Styled file input
    $('.file-styled').uniform({
        fileButtonClass: 'action btn bg-blue'
    });
    
});
