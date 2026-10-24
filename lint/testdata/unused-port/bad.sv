module top (input clk, input ignored, // want
            input [3:0] data, output logic [3:0] q,
            inout pad);               // want
  always_ff @(posedge clk) q <= data;
endmodule
