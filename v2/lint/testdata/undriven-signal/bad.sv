module top (input clk, output logic q);
  logic floating;          // want
  wire  loose;             // want
  logic [3:0] bus;         // want
  always_ff @(posedge clk) q <= floating ^ loose ^ bus[0];
endmodule
